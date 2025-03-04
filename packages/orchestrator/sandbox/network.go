package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// on single host there should not be too much network
const MaxNetworkNumber = 256 * 60

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
		if route.Dst == nil && route.Gw != nil {
			link, err := netlink.LinkByIndex(route.LinkIndex)
			if err != nil {
				return "", fmt.Errorf("error fetching interface for default gateway: %w", err)
			}

			return link.Attrs().Name, nil
		}
	}

	return "", fmt.Errorf("cannot find default gateway")
}

type FcNetworkManager struct {
	mu     sync.Mutex
	nextID int64
	// free contains the idle net namespace
	free []int64
}

type FcNetwork struct {
	netNsName string
	idx       int64
	sandboxID string
}

func GetFcNetNsName(sandboxID string) string {
	// ci means code interpreter
	return "ci-" + sandboxID
}

func NewFcNetworkManager() *FcNetworkManager {
	// start from 1
	return &FcNetworkManager{nextID: 1}
}

func (m *FcNetworkManager) NewFcNetwork(sandboxID string) (*FcNetwork, error) {
	var idx int64
	m.mu.Lock()
	if len(m.free) > 0 {
		idx = m.free[0]
		m.free = m.free[1:]
	} else {
		idx = m.nextID
		m.nextID += 1
	}
	m.mu.Unlock()
	if idx > MaxNetworkNumber {
		return nil, fmt.Errorf("network instance number exceed the upper bound")
	}
	netNsName := GetFcNetNsName(sandboxID)
	return &FcNetwork{
		netNsName: netNsName,
		idx:       idx,
		sandboxID: sandboxID,
	}, nil
}

// Typically used to search network information for orphan sandbox.
// For more, check orchestrator grpc about orphan sandbox.
func (nm *FcNetworkManager) SearchFcNetworkByID(ctx context.Context, tracer trace.Tracer, sandboxID string) (*FcNetwork, error) {
	childCtx, childSpan := tracer.Start(ctx, "search-fc-network-by-id", trace.WithAttributes(
		attribute.String("sandbox.id", sandboxID),
	))
	defer childSpan.End()
	// netns of sandbox
	netNsName := GetFcNetNsName(sandboxID)
	netNsHandle, err := netns.GetFromName(netNsName)
	if err != nil {
		errMsg := fmt.Errorf("get sandbox netns handle failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}
	defer netNsHandle.Close()
	netNsIdx, err := netlink.GetNetNsIdByFd(int(netNsHandle))
	if err != nil {
		errMsg := fmt.Errorf("get sandbox netns index failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}

	// iterate through all the veth devices
	// and try to match their netns link with the target netns
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		veth, ok := link.(*netlink.Veth)
		if !ok {
			continue
		}
		if veth.NetNsID == netNsIdx {
			// we find the veth device!
			fcNetIdx := getFcNetIdxFromVethName(veth.Name)
			return &FcNetwork{
				netNsName: netNsName,
				idx:       int64(fcNetIdx),
				sandboxID: sandboxID,
			}, nil
		}
	}
	return nil, fmt.Errorf("do not find matched fc network for sandbox")
}

func (n *FcNetwork) VethIP() string {
	// as veth mask is 30, which means every 4 address will be in different subnet
	// NOTE each component in ip address is at most 8bit (i.e., 256)
	lower := n.idx % (256 >> (32 - n.VMask()))
	rem := (n.idx - lower) / (256 >> (32 - n.VMask()))
	middle := rem % 256
	higher := (rem - middle) / 256

	// base address is 10.168.0.0
	return fmt.Sprintf("10.%d.%d.%d", 168+higher, middle, lower<<(32-n.VMask())+1)
}

// The veth device ip address in Fc netns
func (n *FcNetwork) VpeerIP() string {
	// as veth mask is 30, which means every 4 address will be in different subnet
	// NOTE each component in ip address is at most 8bit (i.e., 256)
	low := n.idx % (256 >> (32 - n.VMask()))
	rem := (n.idx - low) / (256 >> (32 - n.VMask()))
	middle := rem % 256
	high := (rem - middle) / 256

	// base address is 10.168.0.0
	return fmt.Sprintf("10.%d.%d.%d", 168+high, middle, low<<(32-n.VMask())+2)
}

func (n *FcNetwork) VMask() int {
	return 30
}

// The veth device name in Fc netns
func (n *FcNetwork) VpeerName() string {
	return "veth0"
}

// CIDR format: ip/mask (e.g., 192.168.0.1/24)
//
// The veth device ip address ON HOST
func (n *FcNetwork) VethCIDR() string {
	return fmt.Sprintf("%s/%d", n.VethIP(), n.VMask())
}

func (n *FcNetwork) VethName() string {
	return fmt.Sprintf("veth-ci-%d", n.idx)
}

// return -1 when meet invalid veth name
func getFcNetIdxFromVethName(vethName string) int {
	idx, err := strconv.Atoi(strings.TrimPrefix(vethName, "veth-ci-"))
	if err != nil {
		return -1
	}
	return idx
}

// The veth device ip address in Fc netns
func (n *FcNetwork) VpeerCIDR() string {
	return fmt.Sprintf("%s/%d", n.VpeerIP(), n.VMask())
}

// The tap device addree
func (n *FcNetwork) TapIP() string {
	return consts.FcTapAddress
}

// The tap device addree
func (n *FcNetwork) TapName() string {
	return consts.FcTapName
}

// The tap device addree
func (n *FcNetwork) TapCIDR() string {
	return fmt.Sprintf("%s/%s", n.TapIP(), consts.FcTapMask)
}

// The ip address of the guest OS
func (n *FcNetwork) GuestIP() string {
	return consts.FcAddr
}

// Difference instances of sandbox will have same ip address,
// because they are restored from the same snapshot, and during
// snapshot creation, they are all configured with the same ip address (in guest).
// We need a rule in iptable NAT table to change the source ip address from each
// instance, to a seperate ip address (named HostClonedIP) before the network packet
// leaving the guest VM's namespace.
// With that, we can access difference sandbox with differnet ip.
//
// We can take this HostClonedIP as the ip address of each VM from the view on host.
func (n *FcNetwork) HostClonedIP() string {
	low := n.idx%254 + 1 // range from [1, 254]
	high := n.idx / 254
	return fmt.Sprintf("192.168.%d.%d", 168+high, low)
}

func (n *FcNetwork) HostClonedCIDR() string {
	return fmt.Sprintf("%s/%d", n.HostClonedIP(), 32)
}

func (n *FcNetwork) Setup(ctx context.Context, tracer trace.Tracer, dns *DNS) error {
	childCtx, childSpan := tracer.Start(ctx, "create-network", trace.WithAttributes(
		attribute.Int64("net.index", n.idx),
		attribute.String("sandbox.veth.cidr", n.VethCIDR()),
		attribute.String("sandbox.vpeer.cidr", n.VpeerCIDR()),
		attribute.String("sandbox.tap.cidr", n.TapCIDR()),
		attribute.String("sandbox.host_cloned.cidr", n.HostClonedCIDR()),
		attribute.String("sandbox.guest.ip", n.GuestIP()),
		attribute.String("sandbox.tap.ip", n.TapIP()),
		attribute.String("sandbox.tap.name", n.TapName()),
		attribute.String("sandbox.veth.name", n.VethName()),
		attribute.String("sandbox.vpeer.name", n.VpeerName()),
		attribute.String("sandbox.namespace.id", n.NetNsName()),
		attribute.String("sandbox.id", n.sandboxID),
	))
	defer childSpan.End()

	// Prevent thread changes so we can safely manipulate with namespaces
	telemetry.ReportEvent(childCtx, "waiting for OS thread lock")
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	telemetry.ReportEvent(childCtx, "OS thread lock passed")

	// Save the original (host) namespace and restore it upon function exit
	hostNS, err := netns.Get()
	if err != nil {
		errMsg := fmt.Errorf("cannot get current (host) namespace: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Saved original ns")
	defer func() {
		// recover back to root net ns
		err = netns.Set(hostNS)
		if err != nil {
			errMsg := fmt.Errorf("error resetting network namespace back to the host namespace: %w", err)
			telemetry.ReportError(childCtx, errMsg)
		}
		err = hostNS.Close()
		if err != nil {
			errMsg := fmt.Errorf("error closing host network namespace: %w", err)
			telemetry.ReportError(childCtx, errMsg)
		}
	}()

	// Create NS for the env instance
	// after NewNamed we are already in new netns
	ns, err := netns.NewNamed(n.NetNsName())
	if err != nil {
		errMsg := fmt.Errorf("cannot create new namespace: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Created ns")
	defer ns.Close()

	// Create the Veth and Vpeer
	// Veth: put into host netns
	// Vpeer: put into guest netns
	vethAttrs := netlink.NewLinkAttrs()
	vethAttrs.Name = n.VethName()
	vethAttrs.Namespace = netlink.NsFd(hostNS)
	veth := &netlink.Veth{
		LinkAttrs: vethAttrs,
		PeerName:  n.VpeerName(),
	}
	err = netlink.LinkAdd(veth)
	if err != nil {
		errMsg := fmt.Errorf("error creating veth device: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Created veth device")

	vpeer, err := netlink.LinkByName(n.VpeerName())
	if err != nil {
		errMsg := fmt.Errorf("error finding vpeer: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Linked veth")

	err = netlink.LinkSetUp(vpeer)
	if err != nil {
		errMsg := fmt.Errorf("error setting vpeer device up: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Set up veth")

	ip, ipNet, err := net.ParseCIDR(n.VpeerCIDR())
	if err != nil {
		errMsg := fmt.Errorf("error parsing vpeer CIDR: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Parsed CIDR")

	err = netlink.AddrAdd(vpeer, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: ipNet.Mask,
		},
	})
	if err != nil {
		errMsg := fmt.Errorf("error adding vpeer device address: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Added veth address")

	// Start configure veth (in the host ns)
	err = netns.Set(hostNS)
	if err != nil {
		errMsg := fmt.Errorf("error setting network namespace: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Set ns to host ns")

	err = netlink.LinkSetUp(veth)
	if err != nil {
		errMsg := fmt.Errorf("error setting veth device up: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Set veth device up")

	ip, ipNet, err = net.ParseCIDR(n.VethCIDR())
	if err != nil {
		errMsg := fmt.Errorf("error parsing veth  CIDR: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Parsed CIDR")

	err = netlink.AddrAdd(veth, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: ipNet.Mask,
		},
	})
	if err != nil {
		errMsg := fmt.Errorf("error adding veth device address: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Added veth device address")

	err = netns.Set(ns)
	if err != nil {
		errMsg := fmt.Errorf("error setting network namespace to %s: %w", ns.String(), err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Set network namespace")

	// Create Tap device for FC in NS
	tapAttrs := netlink.NewLinkAttrs()
	tapAttrs.Name = n.TapName()
	tapAttrs.Namespace = ns
	tap := &netlink.Tuntap{
		Mode:      netlink.TUNTAP_MODE_TAP,
		LinkAttrs: tapAttrs,
	}
	err = netlink.LinkAdd(tap)
	if err != nil {
		errMsg := fmt.Errorf("error creating tap device: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Created tap device")

	err = netlink.LinkSetUp(tap)
	if err != nil {
		errMsg := fmt.Errorf("error setting tap device up: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Set tap device up")

	ip, ipNet, err = net.ParseCIDR(n.TapCIDR())
	if err != nil {
		errMsg := fmt.Errorf("error parsing tap CIDR: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Parsed CIDR")

	err = netlink.AddrAdd(tap, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: ipNet.Mask,
		},
	})
	if err != nil {
		errMsg := fmt.Errorf("error setting address of the tap device: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Set tap device address")

	// Set NS lo device up
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		errMsg := fmt.Errorf("error finding lo: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Found lo")

	err = netlink.LinkSetUp(lo)
	if err != nil {
		errMsg := fmt.Errorf("error setting lo device up: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Set lo device up")

	// Add default route in guest ns
	err = netlink.RouteAdd(&netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Gw:    net.ParseIP(n.VethIP()),
	})
	if err != nil {
		errMsg := fmt.Errorf("error adding default NS route: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Added default ns route")

	// This iptables is in guest netns
	tables, err := iptables.New()
	if err != nil {
		errMsg := fmt.Errorf("error initializing iptables: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Initialized iptables")

	// Add NAT routing rules to guest netns (the high-level guideline can
	// be found in firecracker doc: network-for-clones.md
	// 1. (GuestNS) the packet send out from vpeer with guest OS IP address need change to host cloned address
	err = tables.Append("nat", "POSTROUTING", "-o", n.VpeerName(),
		"-s", n.GuestIP(), "-j", "SNAT",
		"--to-source", n.HostClonedIP(),
	)
	if err != nil {
		errMsg := fmt.Errorf("error creating postrouting rule to vpeer: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Created postrouting rule to vpeer")

	// 2. (GuestNS) the packet send to host cloned address needed to be route backed to the guest OS
	// the guest OS ip (the same subnet with tap) will route through tap device
	err = tables.Append("nat", "PREROUTING", "-i", n.VpeerName(),
		"-d", n.HostClonedIP(), "-j", "DNAT",
		"--to-destination", n.GuestIP(),
	)
	if err != nil {
		errMsg := fmt.Errorf("error creating postrouting rule from vpeer: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Created postrouting rule from vpeer")

	// Go back to original namespace
	err = netns.Set(hostNS)
	if err != nil {
		errMsg := fmt.Errorf("error setting network namespace to %s: %w", hostNS.String(), err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Set network namespace back")

	// 3. (HostNS) Need a route entry in host, to route host cloned ip through veth
	_, ipNet, err = net.ParseCIDR(n.HostClonedCIDR())
	if err != nil {
		errMsg := fmt.Errorf("error parsing host snapshot CIDR: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Parsed CIDR")

	err = netlink.RouteAdd(&netlink.Route{
		// Gw means next hop
		Gw:  net.ParseIP(n.VpeerIP()),
		Dst: ipNet,
	})
	if err != nil {
		errMsg := fmt.Errorf("error adding route from host to FC: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Added route from host to FC")

	// 4. (HostNS) Need add FORWARD entries in iptables, to allow packet from veth to outside and
	//             from outside to veth (routed through host to guest, or from guest)
	err = tables.Append("filter", "FORWARD", "-i", n.VethName(), "-o", hostDefaultGateway, "-j", "ACCEPT")
	if err != nil {
		errMsg := fmt.Errorf("error creating forwarding rule to default gateway: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Created forwarding rule to default gateway")

	err = tables.Append("filter", "FORWARD", "-i", hostDefaultGateway, "-o", n.VethName(), "-j", "ACCEPT")
	errMsg := fmt.Errorf("error creating forwarding rule from default gateway: %w", err)
	if err != nil {
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Created forwarding rule from default gateway")

	// 5. (HostNS) Add host postrouting rules, change packet source ip address is it is from host cloned ip
	// to make guest can connected to outside internet
	err = tables.Append("nat", "POSTROUTING", "-s", n.HostClonedIP(), "-o", hostDefaultGateway, "-j", "MASQUERADE")
	if err != nil {
		errMsg := fmt.Errorf("error creating postrouting rule: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Created postrouting rule")

	// 6. (HostNS) Add entry to etc hosts
	err = dns.Add(n.HostClonedIP(), n.sandboxID)
	if err != nil {
		errMsg := fmt.Errorf("error adding env instance to etc hosts: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "Added env instance to etc hosts")

	return nil
}

func (n *FcNetwork) Cleanup(ctx context.Context, tracer trace.Tracer, dns *DNS) error {
	childCtx, childSpan := tracer.Start(ctx, "create-network", trace.WithAttributes(
		attribute.Int64("net.index", n.idx),
		attribute.String("sandbox.id", n.sandboxID),
	))
	defer childSpan.End()
	var finalErr error

	err := dns.Remove(n.sandboxID)
	if err != nil {
		errMsg := fmt.Errorf("error removing env instance to etc hosts: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed env instance to etc hosts")
	}

	tables, err := iptables.New()
	if err != nil {
		errMsg := fmt.Errorf("error initializing iptables: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "initialized iptables")

		err = tables.Delete("filter", "FORWARD", "-i", n.VethName(), "-o", hostDefaultGateway, "-j", "ACCEPT")
		if err != nil {
			errMsg := fmt.Errorf("error deleting host forwarding rule to default gateway: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)
			finalErr = errors.Join(finalErr, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "deleted host forwarding rule to default gateway")
		}

		err = tables.Delete("filter", "FORWARD", "-i", hostDefaultGateway, "-o", n.VethName(), "-j", "ACCEPT")
		if err != nil {
			errMsg := fmt.Errorf("error deleting host forwarding rule from default gateway: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)
			finalErr = errors.Join(finalErr, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "deleted host forwarding rule from default gateway")
		}

		// Delete host postrouting rules
		err = tables.Delete("nat", "POSTROUTING", "-s", n.HostClonedIP(), "-o", hostDefaultGateway, "-j", "MASQUERADE")
		if err != nil {
			errMsg := fmt.Errorf("error deleting host postrouting rule: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)
			finalErr = errors.Join(finalErr, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "deleted host postrouting rule")
		}
	}

	// Delete routing from host to FC namespace
	_, ipNet, err := net.ParseCIDR(n.HostClonedCIDR())
	if err != nil {
		errMsg := fmt.Errorf("error parsing host snapshot CIDR: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "parsed CIDR")

		err = netlink.RouteDel(&netlink.Route{
			Gw:  net.ParseIP(n.VpeerIP()),
			Dst: ipNet,
		})
		if err != nil {
			errMsg := fmt.Errorf("error deleting route from host to FC: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)
			finalErr = errors.Join(finalErr, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "deleted route from host to FC")
		}
	}

	// Delete veth device
	// We explicitly delete the veth device from the host namespace because even though deleting
	// is deleting the device there may be a race condition when creating a new veth device with
	// the same name immediately after deleting the namespace.
	veth, err := netlink.LinkByName(n.VethName())
	if err != nil {
		errMsg := fmt.Errorf("error finding veth: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "found veth")

		err = netlink.LinkDel(veth)
		if err != nil {
			errMsg := fmt.Errorf("error deleting veth device: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)
			finalErr = errors.Join(finalErr, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "deleted veth device")
		}
	}

	err = netns.DeleteNamed(n.NetNsName())
	if err != nil {
		errMsg := fmt.Errorf("error deleting namespace: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "deleted namespace")
	}

	return finalErr
}

func (n *FcNetwork) FcNetworkIdx() int64 {
	return n.idx
}

func (n *FcNetwork) NetNsName() string {
	return n.netNsName
}
