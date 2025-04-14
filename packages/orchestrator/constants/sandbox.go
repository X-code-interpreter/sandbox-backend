package constants

import "github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"

const (
	FcBinaryPath          = "/root/codes/firecracker/build/cargo_target/x86_64-unknown-linux-musl/release/firecracker"
	ChBinaryPath          = "/root/codes/cloud-hypervisor/target/x86_64-unknown-linux-musl/release/cloud-hypervisor"
	PrometheusTargetsPath = consts.MntPrefix + "/prometheus-targets"
)
