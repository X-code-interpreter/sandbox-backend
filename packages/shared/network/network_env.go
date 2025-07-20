package network

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
)

type NetworkEnv struct {
	// The internal index (instead of the id of network namespace)
	idx int
	// Subnet of the veth and vpeer device
	subnet *net.IPNet
}

func NewNetworkEnv(idx int, subnet *net.IPNet) NetworkEnv {
	return NetworkEnv{idx, subnet}
}

func (n *NetworkEnv) NetNsName() string {
	// NOTE: we encode the ipnet into its name
	// to prevent conflict from different subnet.
	ip := strings.ReplaceAll(n.subnet.IP.String(), ".", "-")
	maskSize, _ := n.subnet.Mask.Size()
	return fmt.Sprintf("sandbox-net-%s-%d-%d", ip, maskSize, n.idx)
}

// return -1 when meet invalid netns name
func ParseNetworkEnvFromNetNsName(netNsName string) (*NetworkEnv, error) {
	prefix := "sandbox-net-"
	if !strings.HasPrefix(netNsName, prefix) {
		return nil, fmt.Errorf("invalid netns name prefix: %s", netNsName)
	}
	parts := strings.Split(strings.TrimPrefix(netNsName, prefix), "-")
	if len(parts) != 6 {
		return nil, fmt.Errorf("invalid netns name format: %s", netNsName)
	}

	ipStr := strings.Join(parts[0:4], ".")
	_, ipnet, err := net.ParseCIDR(ipStr + "/" + parts[4])
	if err != nil {
		return nil, fmt.Errorf("parse subnet for %s failed: %w", netNsName, err)
	}
	idx, err := strconv.Atoi(parts[5])
	if err != nil {
		return nil, fmt.Errorf("invalid index: %v", err)
	}

	return &NetworkEnv{idx: idx, subnet: ipnet}, nil
}

func (n *NetworkEnv) NetworkIdx() int {
	return n.idx
}

// The veth device ip address in host netns
func (n *NetworkEnv) VethIP() net.IP {
	// NOTE: Currently, only support ipv4 addr
	baseIPAddr := binary.BigEndian.Uint32(n.subnet.IP.To4())
	ones, bits := n.subnet.Mask.Size()
	size := 1 >> (bits - ones)
	offset := size * n.idx

	result := make(net.IP, 4)
	binary.BigEndian.PutUint32(result, baseIPAddr+uint32(offset)+1)
	return result
}

// The veth device ip address in sandbox netns
func (n *NetworkEnv) VpeerIP() net.IP {
	// NOTE: Currently, only support ipv4 addr
	baseIPAddr := binary.BigEndian.Uint32(n.subnet.IP.To4())
	ones, bits := n.subnet.Mask.Size()
	size := 1 >> (bits - ones)
	offset := size * n.idx

	result := make(net.IP, 4)
	binary.BigEndian.PutUint32(result, baseIPAddr+uint32(offset)+2)
	return result
}

func (n *NetworkEnv) VethMask() int {
	return consts.VethMask
}

// The veth device name in Fc netns
func (n *NetworkEnv) VpeerName() string {
	return consts.VPeerName
}

// CIDR format: ip/mask (e.g., 192.168.0.1/24)
//
// The veth device ip address ON HOST
func (n *NetworkEnv) VethCIDR() string {
	return fmt.Sprintf("%s/%d", n.VethIP(), n.VethMask())
}

func (n *NetworkEnv) VethName() string {
	return fmt.Sprintf("veth-ci-%d", n.idx)
}

// The veth device ip address in Fc netns
func (n *NetworkEnv) VpeerCIDR() string {
	return fmt.Sprintf("%s/%d", n.VpeerIP(), n.VethMask())
}

// The tap device addree
func (n *NetworkEnv) TapIP() net.IP {
	return net.ParseIP(consts.HostTapIPAddress)
}

// The tap device addree
func (n *NetworkEnv) TapName() string {
	return consts.HostTapName
}

// The tap device addree
func (n *NetworkEnv) TapCIDR() string {
	return fmt.Sprintf("%s/%s", n.TapIP(), consts.HostTapIPMask)
}

// The ip address of the guest OS
func (n *NetworkEnv) GuestIP() string {
	return consts.GuestNetIPAddr
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
	// TODO: remove host cloned ip and use veth address directly?
	return fmt.Sprintf("192.168.%d.%d", 168+high, low)
}

func (n *NetworkEnv) HostClonedCIDR() string {
	return fmt.Sprintf("%s/%d", n.HostClonedIP(), 32)
}
