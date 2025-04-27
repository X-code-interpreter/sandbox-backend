package constants

import "github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"

const (
	FcBinaryName          = "firecracker"
	FcBinaryPath          = "/root/codes/firecracker/build/cargo_target/x86_64-unknown-linux-musl/release/firecracker"
	ChBinaryName          = "cloud-hypervisor"
	ChBinaryPath          = "/root/codes/cloud-hypervisor/target/x86_64-unknown-linux-musl/release/cloud-hypervisor"
	PrometheusTargetsPath = consts.MntPrefix + "/prometheus-targets"

	// on single host there should not be too much network
	MaxNetworkNumber = 256 * 60
)
