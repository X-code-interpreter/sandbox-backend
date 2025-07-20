package consts

const (
	KernelName = "vmlinux"

	TemplateDirName = "templates"
	KernelDirName   = "kernels"

	GuestEnvdPath = "/usr/bin/envd"

	DefaultKernelVersion = "6.1.134"

	RootfsName       = "rootfs.ext4"          // the base image
	WritableFsName   = "writable-rootfs.ext4" // an empty writable image
	TemplateFileName = "template.toml"
)
