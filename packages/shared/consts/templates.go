package consts

const (
	EnvsDisk       = "/fc-envs"
	KernelsDir     = "/fc-kernels" // the kernel resides on host
	KernelMountDir = "/fc-vm"      // the kernel resides in the per-fc mount ns
	KernelName     = "vmlinux"

	HostEnvdPath  = "/fc-vm/envd"
	GuestEnvdPath = "/usr/bin/envd"

  DefaultKernelVersion = "5.10.186"
)
