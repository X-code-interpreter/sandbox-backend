package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"slices"
	"syscall"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"go.opentelemetry.io/otel/attribute"
)

var hostDefaultGateway = Must(getDefaultGateway())

func Must[T any](obj T, err error) T {
	if err != nil {
		panic(err)
	}

	return obj
}

func getDefaultGateway() (string, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return "", fmt.Errorf("error fetching routes: %w", err)
	}

	for _, route := range routes {
		if route.Dst == nil || route.Dst.String() == "0.0.0.0/0" || route.Dst.String() == "::/0" {
			if route.Gw != nil && len(route.Gw) > 0 {
				link, err := netlink.LinkByIndex(route.LinkIndex)
				if err != nil {
					return "", fmt.Errorf("error fetching interface for default gateway: %w", err)
				}
				return link.Attrs().Name, nil
			}
		}
	}

	return "", fmt.Errorf("cannot find default gateway")
}

// WARNING: Please lock the os thread when using network env
// runtime.LockOSThread()
// defer runtime.UnlockOSThread()
type SandboxNetwork struct {
	NetworkEnv
	SandboxID string

	hostNS       netns.NsHandle
	sbxNs        netns.NsHandle
	unlockThread bool
	cleanup      []func() error
}

func NewSandboxNetwork(env NetworkEnv, sandboxID string) SandboxNetwork {
	return SandboxNetwork{
		NetworkEnv: env,
		SandboxID:  sandboxID,
		hostNS:     netns.None(),
		sbxNs:      netns.None(),
		cleanup:    []func() error{},
	}
}

func (n *SandboxNetwork) SetSandboxNs() error {
	return netns.Set(n.sbxNs)
}

func (n *SandboxNetwork) SetHostNs() error {
	return netns.Set(n.hostNS)
}

// WARNING: Please make sure to call EndConfigure()
//
// when return, we are in sandbox ns
func (n *SandboxNetwork) StartConfigure() error {
	// makesure the netns not exist yet
	ns, err := netns.GetFromName(n.NetNsName())
	if err == nil {
		ns.Close()
		return fmt.Errorf("netns for %d already exists", n.idx)
	} else if !errors.Is(err, syscall.ENOENT) {
		return fmt.Errorf("get netns by name error: %w", err)
	}

	runtime.LockOSThread()
	n.unlockThread = true
	hostNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("cannot get current (host) namespace: %w", err)
	}
	n.hostNS = hostNS
	sbxNs, err := netns.NewNamed(n.NetNsName())
	if err != nil {
		return fmt.Errorf("cannot create new namespace: %w", err)
	}
	n.sbxNs = sbxNs
	n.cleanup = append(n.cleanup, n.DeleteNetns)

	return nil
}

func (n *SandboxNetwork) EndConfigure() error {
	if n.unlockThread {
		defer runtime.UnlockOSThread()
	}
	if n.hostNS.IsOpen() {
		if err := netns.Set(n.hostNS); err != nil {
			return fmt.Errorf("set back to host netns failed: %w", err)
		}
		if err := n.hostNS.Close(); err != nil {
			return err
		}
	}
	if n.sbxNs.IsOpen() {
		if err := n.sbxNs.Close(); err != nil {
			return err
		}
	}
	return nil
}

// start at sandbox ns
// end at sandbox ns
func (n *SandboxNetwork) SetupSbxTapDev() error {
	// Create Tap device in guest NS
	tapAttrs := netlink.NewLinkAttrs()
	tapAttrs.Name = n.TapName()
	tapAttrs.Namespace = netlink.NsFd(n.sbxNs)
	tap := &netlink.Tuntap{
		Mode:      netlink.TUNTAP_MODE_TAP,
		LinkAttrs: tapAttrs,
	}
	err := netlink.LinkAdd(tap)
	if err != nil {
		return fmt.Errorf("error creating tap device: %w", err)
	}

	err = netlink.LinkSetUp(tap)
	if err != nil {
		return fmt.Errorf("error setting tap device up: %w", err)
	}

	// setup ip of tap dev
	ip, ipNet, err := net.ParseCIDR(n.TapCIDR())
	if err != nil {
		return fmt.Errorf("error parsing tap CIDR: %w", err)
	}

	err = netlink.AddrAdd(tap, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: ipNet.Mask,
		},
	})
	if err != nil {
		return fmt.Errorf("error setting address of the tap device: %w", err)
	}

	return nil
}

// start at sandbox ns
// end at sandbox ns
func (n *SandboxNetwork) SetupSbxLoDev() error {
	// Set NS lo device up
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("error finding lo: %w", err)
	}

	err = netlink.LinkSetUp(lo)
	if err != nil {
		return fmt.Errorf("error setting lo device up: %w", err)
	}

	return nil
}

// Start at sandbox netns
// end at host netns
func (n *SandboxNetwork) SetupVethPair() error {
	// Create the Veth and Vpeer
	// Veth: put into host netns
	// Vpeer: put into sandbox netns
	vethAttrs := netlink.NewLinkAttrs()
	vethAttrs.Name = n.VethName()
	vethAttrs.Namespace = netlink.NsFd(n.hostNS)
	veth := &netlink.Veth{
		LinkAttrs: vethAttrs,
		PeerName:  n.VpeerName(),
	}
	err := netlink.LinkAdd(veth)
	if err != nil {
		return fmt.Errorf("error creating veth device: %w", err)
	}

	n.cleanup = append(n.cleanup, n.DeleteHostVethDev)

	vpeer, err := netlink.LinkByName(n.VpeerName())
	if err != nil {
		return fmt.Errorf("error finding vpeer %s: %w", n.VpeerName(), err)
	}

	err = netlink.LinkSetUp(vpeer)
	if err != nil {
		return fmt.Errorf("error setting vpeer device up: %w", err)
	}

	ip, ipNet, err := net.ParseCIDR(n.VpeerCIDR())
	if err != nil {
		return fmt.Errorf("error parsing vpeer CIDR %s: %w", n.VpeerCIDR(), err)
	}
	err = netlink.AddrAdd(vpeer, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: ipNet.Mask,
		},
	})
	if err != nil {
		return fmt.Errorf("error adding vpeer device address: %w", err)
	}

	// Start configure veth (in the host ns)
	err = n.SetHostNs()
	if err != nil {
		return fmt.Errorf("error setting to host ns: %w", err)
	}

	err = netlink.LinkSetUp(veth)
	if err != nil {
		return fmt.Errorf("error setting veth device up: %w", err)
	}

	ip, ipNet, err = net.ParseCIDR(n.VethCIDR())
	if err != nil {
		return fmt.Errorf("error parsing veth CIDR %s: %w", n.VethCIDR(), err)
	}

	err = netlink.AddrAdd(veth, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: ipNet.Mask,
		},
	})
	if err != nil {
		return fmt.Errorf("error adding veth device address: %w", err)
	}

	return nil
}

// Start at sandbox ns
// end at hostns
func (n *SandboxNetwork) SetupIptablesAndRoute() error {
	// Add default route in sandbox ns
	err := netlink.RouteAdd(&netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Gw:    net.ParseIP(n.VethIP()),
	})
	if err != nil {
		return fmt.Errorf("error adding default NS route: %w", err)
	}

	n.cleanup = append(n.cleanup, n.DeleteHostRoute)

	// This iptables is in sandbox netns
	tables, err := iptables.New()
	if err != nil {
		return fmt.Errorf("error initializing iptables in guest netns: %w", err)
	}

	n.cleanup = append(n.cleanup, n.DeleteHostIptables)

	// Add NAT routing rules to sandbox netns: the high-level guideline can
	// be found in firecracker doc: network-for-clones.md
	// 1. (SandboxNS) the packet send out from vpeer with guest OS IP address need change to host cloned address
	err = tables.Append("nat", "POSTROUTING", "-o", n.VpeerName(),
		"-s", n.GuestIP(), "-j", "SNAT",
		"--to-source", n.HostClonedIP(),
	)
	if err != nil {
		return fmt.Errorf("error creating postrouting rule for packet leaving guest: %w", err)
	}

	// 2. (SandboxNS) the packet send to host cloned address needed to be route backed to the guest OS
	// the guest OS ip (the same subnet with tap) will route through tap device
	err = tables.Append("nat", "PREROUTING", "-i", n.VpeerName(),
		"-d", n.HostClonedIP(), "-j", "DNAT",
		"--to-destination", n.GuestIP(),
	)
	if err != nil {
		return fmt.Errorf("error creating postrouting rule for packet targeting guest: %w", err)
	}

	// Go back to host network namespace
	err = n.SetHostNs()
	if err != nil {
		return fmt.Errorf("error setting to host ns: %w", err)
	}

	// 3. (HostNS) Need a route entry in host, to route host cloned ip through veth
	_, ipNet, err := net.ParseCIDR(n.HostClonedCIDR())
	if err != nil {
		return fmt.Errorf("error parsing host snapshot CIDR %s: %w", n.HostClonedCIDR(), err)
	}

	err = netlink.RouteAdd(&netlink.Route{
		// Gw means next hop
		Gw:  net.ParseIP(n.VpeerIP()),
		Dst: ipNet,
	})
	if err != nil {
		return fmt.Errorf("error adding route from host to guest vpeer: %w", err)
	}

	// 4. (HostNS) Need add FORWARD entries in iptables, to allow packet from veth to outside and
	//             from outside to veth (routed through host to guest, or from guest)
	err = tables.Append("filter", "FORWARD", "-i", n.VethName(), "-o", hostDefaultGateway, "-j", "ACCEPT")
	if err != nil {
		return fmt.Errorf("error creating forwarding rule to packet leaving host default gateway: %w", err)
	}

	err = tables.Append("filter", "FORWARD", "-i", hostDefaultGateway, "-o", n.VethName(), "-j", "ACCEPT")
	if err != nil {
		return fmt.Errorf("error creating forwarding rule to packet coming from default gateway: %w", err)
	}

	// 5. (HostNS) Add host postrouting rules, change packet source ip address is it is from host cloned ip
	// to make guest can connected to outside internet
	err = tables.Append("nat", "POSTROUTING", "-s", n.HostClonedIP(), "-o", hostDefaultGateway, "-j", "MASQUERADE")
	if err != nil {
		return fmt.Errorf("error creating postrouting rule to packet leaving host default gateway: %w", err)
	}

	return nil
}

func (n *SandboxNetwork) Cleanup(ctx context.Context) error {
	var finalErr error

	// apply cleanup function reversely
	for _, f := range slices.Backward(n.cleanup) {
		if err := f(); err != nil {
			telemetry.ReportCriticalError(ctx, err)
			finalErr = errors.Join(finalErr, err)
		}
	}

	telemetry.ReportEvent(ctx, "sandbox network cleanup", attribute.Int64("idx", n.NetworkIdx()))

	return finalErr

	// if err := n.DeleteDNSEntry(dns); err != nil {
	// 	telemetry.ReportCriticalError(ctx, err)
	// 	finalErr = errors.Join(finalErr, err)
	// } else {
	// 	telemetry.ReportEvent(ctx, "removed env instance to etc hosts")
	// }
	//
	// if err := n.DeleteHostIptables(); err != nil {
	// 	telemetry.ReportCriticalError(ctx, err)
	// 	finalErr = errors.Join(finalErr, err)
	// } else {
	// 	telemetry.ReportEvent(ctx, "deleted host iptables rules")
	// }
	//
	// if err := n.DeleteHostRoute(); err != nil {
	// 	telemetry.ReportCriticalError(ctx, err)
	// 	finalErr = errors.Join(finalErr, err)
	// } else {
	// 	telemetry.ReportEvent(ctx, "deleted host route entry")
	// }
	//
	// if err := n.DeleteHostVethDev(); err != nil {
	// 	telemetry.ReportCriticalError(ctx, err)
	// 	finalErr = errors.Join(finalErr, err)
	// } else {
	// 	telemetry.ReportEvent(ctx, "deleted host veth dev")
	// }
	//
	// if err := n.DeleteNetns(); err != nil {
	// 	telemetry.ReportCriticalError(ctx, err)
	// 	finalErr = errors.Join(finalErr, err)
	// } else {
	// 	telemetry.ReportEvent(ctx, "deleted host veth dev")
	// }

	// return finalErr
}

func (n *SandboxNetwork) DeleteHostVethDev() error {
	// Delete veth device
	// We explicitly delete the veth device from the host namespace because even though deleting
	// is deleting the device there may be a race condition when creating a new veth device with
	// the same name immediately after deleting the namespace.
	veth, err := netlink.LinkByName(n.VethName())
	if err != nil {
		return fmt.Errorf("error finding veth: %w", err)
	}
	err = netlink.LinkDel(veth)
	if err != nil {
		return fmt.Errorf("error deleting veth device: %w", err)
	}
	return nil
}

func (n *SandboxNetwork) DeleteHostRoute() (finalErr error) {
	// Delete routing from host to guest namespace
	_, ipNet, err := net.ParseCIDR(n.HostClonedCIDR())
	if err != nil {
		return fmt.Errorf("error parsing host snapshot CIDR: %w", err)
	}
	err = netlink.RouteDel(&netlink.Route{
		Gw:  net.ParseIP(n.VpeerIP()),
		Dst: ipNet,
	})
	if err != nil {
		return fmt.Errorf("error deleting route from host to guest vpeer: %w", err)
	}
	return nil
}

func (n *SandboxNetwork) DeleteHostIptables() (finalErr error) {
	tables, err := iptables.New()
	if err != nil {
		return fmt.Errorf("error initializing iptables: %w", err)
	}
	err = tables.Delete("filter", "FORWARD", "-i", n.VethName(), "-o", hostDefaultGateway, "-j", "ACCEPT")
	if err != nil {
		errMsg := fmt.Errorf("error deleting forwarding rule to packet leaving host default gateway: %w", err)
		finalErr = errors.Join(finalErr, errMsg)
	}

	err = tables.Delete("filter", "FORWARD", "-i", hostDefaultGateway, "-o", n.VethName(), "-j", "ACCEPT")
	if err != nil {
		errMsg := fmt.Errorf("error deleting forwarding rule to packet coming from default gateway: %w", err)
		finalErr = errors.Join(finalErr, errMsg)
	}

	// Delete host postrouting rules
	err = tables.Delete("nat", "POSTROUTING", "-s", n.HostClonedIP(), "-o", hostDefaultGateway, "-j", "MASQUERADE")
	if err != nil {
		errMsg := fmt.Errorf("error deleting postrouting rule to packet leaving host default gateway: %w", err)
		finalErr = errors.Join(finalErr, errMsg)
	}

	return finalErr
}

func (n *SandboxNetwork) DeleteNetns() error {
	ns, err := netns.GetFromName(n.NetNsName())
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			// netns not exists, just return
			return nil
		}
		return err
	}

	// netns exists
	defer ns.Close()
	err = netns.DeleteNamed(n.NetNsName())
	if err != nil {
		return err
	}
	return nil
}
