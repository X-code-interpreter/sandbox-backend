package network

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
)

type NetworkEnv struct {
	// The internal index (instead of the id of network namespace)
	idx int64
}

func NewNetworkEnv(idx int64) NetworkEnv {
	return NetworkEnv{idx}
}

func (n *NetworkEnv) NetNsName() string {
	return "sandbox-net-" + strconv.FormatInt(n.idx, 10)
}

// return -1 when meet invalid veth name
func GetInternalIdxFromNetNsName(netNsName string) int64 {
	idx, err := strconv.ParseInt(strings.TrimPrefix(netNsName, "sandbox-net-"), 10, 64)
	if err != nil {
		return -1
	}
	return idx
}

func (n *NetworkEnv) NetworkIdx() int64 {
	return n.idx
}

// The veth device ip address in host netns
func (n *NetworkEnv) VethIP() string {
	// as veth mask is 30, which means every 4 address will be in different subnet
	// NOTE each component in ip address is at most 8bit (i.e., 256)
	lower := n.idx % (256 >> (32 - n.VMask()))
	rem := (n.idx - lower) / (256 >> (32 - n.VMask()))
	middle := rem % 256
	higher := (rem - middle) / 256

	// base address is 10.168.0.0
	return fmt.Sprintf("10.%d.%d.%d", 168+higher, middle, lower<<(32-n.VMask())+1)
}

// The veth device ip address in sandbox netns
func (n *NetworkEnv) VpeerIP() string {
	// as veth mask is 30, which means every 4 address will be in different subnet
	// NOTE each component in ip address is at most 8bit (i.e., 256)
	low := n.idx % (256 >> (32 - n.VMask()))
	rem := (n.idx - low) / (256 >> (32 - n.VMask()))
	middle := rem % 256
	high := (rem - middle) / 256

	// base address is 10.168.0.0
	return fmt.Sprintf("10.%d.%d.%d", 168+high, middle, low<<(32-n.VMask())+2)
}

func (n *NetworkEnv) VMask() int {
	return 30
}

// The veth device name in Fc netns
func (n *NetworkEnv) VpeerName() string {
	return "veth0"
}

// CIDR format: ip/mask (e.g., 192.168.0.1/24)
//
// The veth device ip address ON HOST
func (n *NetworkEnv) VethCIDR() string {
	return fmt.Sprintf("%s/%d", n.VethIP(), n.VMask())
}

func (n *NetworkEnv) VethName() string {
	return fmt.Sprintf("veth-ci-%d", n.idx)
}

// The veth device ip address in Fc netns
func (n *NetworkEnv) VpeerCIDR() string {
	return fmt.Sprintf("%s/%d", n.VpeerIP(), n.VMask())
}

// The tap device addree
func (n *NetworkEnv) TapIP() string {
	return consts.HostTapIpAddress
}

// The tap device addree
func (n *NetworkEnv) TapName() string {
	return consts.HostTapName
}

// The tap device addree
func (n *NetworkEnv) TapCIDR() string {
	return fmt.Sprintf("%s/%s", n.TapIP(), consts.HostTapIpMask)
}

// The ip address of the guest OS
func (n *NetworkEnv) GuestIP() string {
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
func (n *NetworkEnv) HostClonedIP() string {
	low := n.idx%254 + 1 // range from [1, 254]
	high := n.idx / 254
	return fmt.Sprintf("192.168.%d.%d", 168+high, low)
}

func (n *NetworkEnv) HostClonedCIDR() string {
	return fmt.Sprintf("%s/%d", n.HostClonedIP(), 32)
}
