package consts

const (
	// NOTE(huang-jl): the EnvsDisk should be xfs, as we rely on reflink
	EnvsDisk       = "/mnt/pmem1/fc-envs"
	KernelsDir     = "/mnt/pmem1/fc-kernels" // the kernel resides on host
	KernelMountDir = "/mnt/pmem1/fc-vm"      // the kernel resides in the per-fc mount ns
	KernelName     = "vmlinux"

	HostEnvdPath  = "/mnt/pmem1/fc-vm/envd"
	GuestEnvdPath = "/usr/bin/envd"

	DefaultKernelVersion = "5.10.186"

	RootfsName   = "rootfs.ext4"
	SnapfileName = "snapfile"
	MemfileName  = "memfile"
)
