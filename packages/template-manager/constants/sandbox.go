package constants

import "time"

const (
	SandboxIDPrefix = "template-manager-"
	NetnsNamePrefix = "fc-build-env-"

	WaitTimeForVmStart  = 10 * time.Second
	WaitTimeForStartCmd = 15 * time.Second
)
