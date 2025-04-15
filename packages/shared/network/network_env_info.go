package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type NetworkEnvInfo struct {
	netNsName string
	idx       int64
	sandboxID string

	cleanup []func() error
}

func NewNetworkEnvInfo(netnsName string, idx int64, sandboxID string) NetworkEnvInfo {
	cleanup := []func() error{}
	return NetworkEnvInfo{netnsName, idx, sandboxID, cleanup}
}

func GetFcNetNsName(sandboxID string) string {
	// ci means code interpreter
	return "ci-" + sandboxID
}

func (n *NetworkEnvInfo) VethIP() string {
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
func (n *NetworkEnvInfo) VpeerIP() string {
	// as veth mask is 30, which means every 4 address will be in different subnet
	// NOTE each component in ip address is at most 8bit (i.e., 256)
	low := n.idx % (256 >> (32 - n.VMask()))
	rem := (n.idx - low) / (256 >> (32 - n.VMask()))
	middle := rem % 256
	high := (rem - middle) / 256

	// base address is 10.168.0.0
	return fmt.Sprintf("10.%d.%d.%d", 168+high, middle, low<<(32-n.VMask())+2)
}

func (n *NetworkEnvInfo) VMask() int {
	return 30
}

// The veth device name in Fc netns
func (n *NetworkEnvInfo) VpeerName() string {
	return "veth0"
}

// CIDR format: ip/mask (e.g., 192.168.0.1/24)
//
// The veth device ip address ON HOST
func (n *NetworkEnvInfo) VethCIDR() string {
	return fmt.Sprintf("%s/%d", n.VethIP(), n.VMask())
}

func (n *NetworkEnvInfo) VethName() string {
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
func (n *NetworkEnvInfo) VpeerCIDR() string {
	return fmt.Sprintf("%s/%d", n.VpeerIP(), n.VMask())
}

// The tap device addree
func (n *NetworkEnvInfo) TapIP() string {
	return consts.HostTapIpAddress
}

// The tap device addree
func (n *NetworkEnvInfo) TapName() string {
	return consts.HostTapName
}

// The tap device addree
func (n *NetworkEnvInfo) TapCIDR() string {
	return fmt.Sprintf("%s/%s", n.TapIP(), consts.HostTapIpMask)
}

// The ip address of the guest OS
func (n *NetworkEnvInfo) GuestIP() string {
	return consts.GuestNetIpAddr
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
func (n *NetworkEnvInfo) HostClonedIP() string {
	low := n.idx%254 + 1 // range from [1, 254]
	high := n.idx / 254
	return fmt.Sprintf("192.168.%d.%d", 168+high, low)
}

func (n *NetworkEnvInfo) HostClonedCIDR() string {
	return fmt.Sprintf("%s/%d", n.HostClonedIP(), 32)
}

func (n *NetworkEnvInfo) Cleanup(ctx context.Context) error {
	var finalErr error

	for _, f := range slices.Backward(n.cleanup) {
		if err := f(); err != nil {
			telemetry.ReportCriticalError(ctx, err)
			finalErr = errors.Join(finalErr, err)
		}
	}

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

func (n *NetworkEnvInfo) DeleteDNSEntry(dns *DNS) error {
	return dns.Remove(n.sandboxID)
}

func (n *NetworkEnvInfo) DeleteHostVethDev() error {
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

func (n *NetworkEnvInfo) DeleteHostRoute() (finalErr error) {
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

func (n *NetworkEnvInfo) DeleteHostIptables() (finalErr error) {
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

func (n *NetworkEnvInfo) DeleteNetns() error {
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

func (n *NetworkEnvInfo) NetworkEnvIdx() int64 {
	return n.idx
}

func (n *NetworkEnvInfo) NetNsName() string {
	return n.netNsName
}
