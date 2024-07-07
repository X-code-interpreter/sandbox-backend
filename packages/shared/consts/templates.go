package consts

const (
	MntPrefix = "/mnt/pmem1"
	// NOTE(huang-jl): the EnvsDisk should be xfs, as we rely on reflink
	EnvsDisk       = MntPrefix + "/fc-envs"
	KernelsDir     = MntPrefix + "/fc-kernels" // the kernel resides on host
	KernelMountDir = MntPrefix + "/fc-vm"      // the kernel resides in the per-fc mount ns
	KernelName     = "vmlinux"

	HostEnvdPath  = MntPrefix + "/fc-vm/envd"
	GuestEnvdPath = "/usr/bin/envd"

	DefaultKernelVersion = "5.10.186"

	RootfsName     = "rootfs.ext4"            // the base image
	WritableFsName = "writetable-rootfs.ext4" // an empty writable image
	SnapfileName   = "snapfile"
	MemfileName    = "memfile"
)
