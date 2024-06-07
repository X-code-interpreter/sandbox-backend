package consts

const (
	NetworkMask string = "/16"
	Subnet      string = "10.168.0.0" + NetworkMask

	CNIConfigDir   = "/etc/cni/net.d"
	CNIBinDir      = "/opt/cni/bin" // should contains host-local, ptp and tc-redirect-tap
	CNINetworkName = "fcnet"

	IfName = "veth0"
)
