package constants

const (
	FcBinaryName = "firecracker"
	ChBinaryName = "cloud-hypervisor"
	// ChBinaryPath          = "/root/codes/cloud-hypervisor/target/x86_64-unknown-linux-musl/release/cloud-hypervisor"
	PrometheusTargetsDirName = "prometheus-targets"

	// on single host there should not be too much network
	MaxNetworkNumber = 256 * 60
)
