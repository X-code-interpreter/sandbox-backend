package network

import (
	"net"
	"runtime/debug"
	"slices"
	"testing"
)

// assert Fatal
func assert(t *testing.T, res bool) {
	if !res {
		t.Log(string(debug.Stack()))
		t.FailNow()
	}
}

func TestFcNetwork(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("10.140.0.0/16")
	var fcNets []*NetworkEnv
	for i := 0; i < 5000; i++ {
		netEnv := NewNetworkEnv(i, ipnet)
		fcNets = append(fcNets, &netEnv)
	}

	hostClonedIps := make(map[string]struct{})
	vethIps := make(map[string]*net.IPNet)
	// we want to make sure that their ip address is valid and not equal
	for _, n := range fcNets {
		// veth
		cidr := n.VethCIDR()
		ip, ipNet, err := net.ParseCIDR(cidr)
		assert(t, err == nil)
		ip = ip.To4()
		assert(t, ip[3] < 255 && ip[3] > 0)
		vip := n.VethIP()
		assert(t, slices.Equal(vip, ip))
		// make sure vip not conflict with others
		_, ok := vethIps[vip.String()]
		assert(t, !ok)
		// make sure ip is not in the same subnetwork
		for otherIp := range vethIps {
			ip := net.ParseIP(otherIp)
			assert(t, !ipNet.Contains(ip))
		}
		vethIps[vip.String()] = ipNet

		// host cloned ip
		hostCIDR := n.HostClonedCIDR()
		hostIp, _, err := net.ParseCIDR(hostCIDR)
		assert(t, err == nil)
		hostIp = hostIp.To4()
		assert(t, hostIp[3] < 255 && hostIp[3] > 0)
		hIp := n.HostClonedIP()
		assert(t, hIp == hostIp.String())
		_, ok = hostClonedIps[hIp]
		assert(t, !ok)
		for otherIp := range hostClonedIps {
			assert(t, hIp != otherIp)
		}
		hostClonedIps[hIp] = struct{}{}
	}
}
