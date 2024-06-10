package consts

const (
	IfName = "veth0"

	FcTapAddress = "169.254.0.22" // the ip address of tap device
	FcTapMask    = "30"
	FcTapName    = "tap0"

	FcAddr       = "169.254.0.21" // the ip address in the guest OS
	FcMask       = "/30"
	FcMacAddress = "02:FC:00:00:00:05"
	FcMaskLong   = "255.255.255.252"
	FcIfaceID    = "eth0"
)
