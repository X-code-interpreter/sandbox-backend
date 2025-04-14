package consts

const (
	MntPrefix = "/mnt/data/X-code-interpreter"
	// NOTE(huang-jl): the EnvsDisk should be xfs, as we rely on reflink
	EnvsDisk       = MntPrefix + "/fc-envs"
	KernelsDir     = MntPrefix + "/fc-kernels" // the kernel resides on host
	KernelMountDir = MntPrefix + "/fc-vm"      // the kernel resides in the per-fc mount ns
	KernelName     = "vmlinux"

	HostEnvdPath  = "/root/codes/sandbox-backend/packages/envd/bin/envd"
	GuestEnvdPath = "/usr/bin/envd"

	DefaultKernelVersion = "5.10.226"

	RootfsName       = "rootfs.ext4"          // the base image
	WritableFsName   = "writable-rootfs.ext4" // an empty writable image
	TemplateFileName = "template.json"
)
