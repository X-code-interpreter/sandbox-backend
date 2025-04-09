package constants

import "time"

const (
	SandboxIDPrefix = "template-manager-"
	NetnsNamePrefix = "fc-build-env-"

	WaitTimeForFCStart  = 10 * time.Second
	WaitTimeForStartCmd = 15 * time.Second
	WaitTimeForFCConfig = 500 * time.Millisecond

	SocketWaitTimeout = 2 * time.Second
)
