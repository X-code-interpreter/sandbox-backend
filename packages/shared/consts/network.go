package consts

const (
	HostTapIPAddress = "169.254.0.22" // the ip address of tap device
	HostTapIPMask    = "30"
	HostTapName      = "tap0"

	GuestNetIPAddr     = "169.254.0.21" // the ip address in the guest OS
	GuestNetIPMask     = "/30"
	GuestMacAddress    = "02:FC:00:00:00:05"
	GuestNetIPMaskLong = "255.255.255.252"
	GuestIfaceName     = "eth0"

	VethMask  int = 30
	VPeerName     = "veth0"
)
