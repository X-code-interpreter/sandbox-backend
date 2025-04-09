package network

import (
	"fmt"
	"net"

	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
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
type NetworkEnv struct {
	*NetworkEnvInfo
	hostNS netns.NsHandle
	ns     netns.NsHandle
}

func (n *NetworkEnv) SetGuestNs() error {
	return netns.Set(n.ns)
}

func (n *NetworkEnv) SetHostNs() error {
	return netns.Set(n.hostNS)
}

// WARNING: Please lock the os thread when using network env
// runtime.LockOSThread()
// defer runtime.UnlockOSThread()
func (info *NetworkEnvInfo) InitEnv() (*NetworkEnv, error) {
	hostNS, err := netns.Get()
	if err != nil {
		return nil, fmt.Errorf("cannot get current (host) namespace: %w", err)
	}
	ns, err := netns.NewNamed(info.NetNsName())
	if err != nil {
		return nil, fmt.Errorf("cannot create new namespace: %w", err)
	}
	info.cleanup = append(info.cleanup, info.DeleteNetns)

	return &NetworkEnv{
		NetworkEnvInfo: info,
		hostNS:         hostNS,
		ns:             ns,
	}, nil
}

// start at guest ns
// end at guest ns
func (n *NetworkEnv) SetupNsTapDev() error {
	// Create Tap device in guest NS
	tapAttrs := netlink.NewLinkAttrs()
	tapAttrs.Name = n.TapName()
	tapAttrs.Namespace = netlink.NsFd(n.ns)
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

// start at guest ns
// end at guest ns
func (n *NetworkEnv) SetupNsLoDev() error {
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

// Start at guest netns
// end at host netns
func (n *NetworkEnv) SetupVethPair() error {
	// Create the Veth and Vpeer
	// Veth: put into host netns
	// Vpeer: put into guest netns
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

	n.NetworkEnvInfo.cleanup = append(n.NetworkEnvInfo.cleanup, n.NetworkEnvInfo.DeleteHostVethDev)

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

// Start at guest ns
// end at hostns
func (n *NetworkEnv) SetupIptablesAndRoute() error {
	// Add default route in guest ns
	err := netlink.RouteAdd(&netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Gw:    net.ParseIP(n.VethIP()),
	})
	if err != nil {
		return fmt.Errorf("error adding default NS route: %w", err)
	}

	n.NetworkEnvInfo.cleanup = append(n.NetworkEnvInfo.cleanup, n.NetworkEnvInfo.DeleteHostRoute)

	// This iptables is in guest netns
	tables, err := iptables.New()
	if err != nil {
		return fmt.Errorf("error initializing iptables in guest netns: %w", err)
	}

	n.NetworkEnvInfo.cleanup = append(n.NetworkEnvInfo.cleanup, n.NetworkEnvInfo.DeleteHostIptables)

	// Add NAT routing rules to guest netns: the high-level guideline can
	// be found in firecracker doc: network-for-clones.md
	// 1. (GuestNS) the packet send out from vpeer with guest OS IP address need change to host cloned address
	err = tables.Append("nat", "POSTROUTING", "-o", n.VpeerName(),
		"-s", n.GuestIP(), "-j", "SNAT",
		"--to-source", n.HostClonedIP(),
	)
	if err != nil {
		return fmt.Errorf("error creating postrouting rule for packet leaving guest: %w", err)
	}

	// 2. (GuestNS) the packet send to host cloned address needed to be route backed to the guest OS
	// the guest OS ip (the same subnet with tap) will route through tap device
	err = tables.Append("nat", "PREROUTING", "-i", n.VpeerName(),
		"-d", n.HostClonedIP(), "-j", "DNAT",
		"--to-destination", n.GuestIP(),
	)
	if err != nil {
		return fmt.Errorf("error creating postrouting rule for packet targeting guest: %w", err)
	}

	// Go back to original namespace
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

func (n *NetworkEnv) CreateDNSEntry(dns *DNS) error {
	// 6. (HostNS) Add entry to etc hosts
	err := dns.Add(n.HostClonedIP(), n.sandboxID)
	if err != nil {
		return fmt.Errorf("error adding env instance to etc hosts: %w", err)
	}

	n.NetworkEnvInfo.cleanup = append(n.NetworkEnvInfo.cleanup, func() error {
		return n.NetworkEnvInfo.DeleteDNSEntry(dns)
	})
	return nil
}

func (n *NetworkEnv) Exit() error {
	if err := n.SetHostNs(); err != nil {
		return fmt.Errorf("cannot setting to host ns: %w", err)
	}
	n.hostNS.Close()
	n.ns.Close()

	return nil
}
