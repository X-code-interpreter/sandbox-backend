package sandbox

import (
	"fmt"
	"net"
	"runtime/debug"
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
	nm := NewFcNetworkManager()
	var fcNets []*FcNetwork
	for i := 0; i < 5000; i++ {
		id := fmt.Sprintf("test-%d", i)
		fcNet, err := nm.NewFcNetwork(id)
		assert(t, err == nil)
		fcNets = append(fcNets, fcNet)
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
		assert(t, ip.String() == vip)
		// make sure vip not conflict with others
		_, ok := vethIps[vip]
		assert(t, !ok)
		// make sure ip is not in the same subnetwork
		for otherIp := range vethIps {
			ip := net.ParseIP(otherIp)
			assert(t, !ipNet.Contains(ip))
		}
		vethIps[vip] = ipNet

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
