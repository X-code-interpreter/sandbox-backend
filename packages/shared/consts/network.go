package consts

const (
	HostTapIpAddress = "169.254.0.22" // the ip address of tap device
	HostTapIpMask    = "30"
	HostTapName      = "tap0"

	GuestNetIpAddr     = "169.254.0.21" // the ip address in the guest OS
	GuestNetIpMask     = "/30"
	GuestMacAddress    = "02:FC:00:00:00:05"
	GuestNetIpMaskLong = "255.255.255.252"
	GuestIfaceName     = "eth0"
)
