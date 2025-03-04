package template

import (
	"path/filepath"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
)

type VmTemplate struct {
	// Unique ID of the env.
	// required
	EnvID string `json:"template"`

	// Command to run when building the env.
	// optional (default: empty)
	StartCmd string `json:"startCmd"`

	// Path to the firecracker binary.
	// optional (default: firecracker)
	FirecrackerBinaryPath string `json:"fcPath"`

	// The number of vCPUs to allocate to the VM.
	// required
	VCpuCount int64 `json:"vcpu"`

	// The amount of RAM memory to allocate to the VM, in MiB.
	// required
	MemoryMB int64 `json:"memMB"`

	// The amount of free disk to allocate to the VM, in MiB.
	// required
	DiskSizeMB int64 `json:"diskMB"`

	// Real size of the rootfs after building the env.
	RootfsSize int64 `json:"rootfsSize"`

	// Version of the kernel.
	// optional
	KernelVersion string `json:"kernelVersion"`

	// Docker Image to used as the base image
	// if it is empty, it will be "e2bdev/code-interpreter:latest"
	// optional
	DockerImage string `json:"dockerImg"`

	// Use local docker image (i.e., do not pull from remote docker registry)
	NoPull bool `json:"noPull"`

	HugePages bool `json:"hugePages,omitempty"`

	// Create two block device for VM. One is read-only lower dir,
	// the other is writable upper dir.
	// Set this to false (by default) will create one read-write block device.
	Overlay bool `json:"overlay"`
}

// Path to the directory where the env is stored.
func (t *VmTemplate) EnvDirPath() string {
	return filepath.Join(consts.EnvsDisk, t.EnvID)
}

// Path to the file where the rootfs is store on host.
// When enable overlay, this store the read-only lower dir of overlay root file.
func (t *VmTemplate) EnvRootfsPath() string {
	return filepath.Join(t.EnvDirPath(), consts.RootfsName)
}

// Path to the file where the writable rootfs is store on host.
// Only valid when enable overlay, this store the writable upper dir of overlay root file.
func (t *VmTemplate) EnvWritableRootfsPath() string {
	return filepath.Join(t.EnvDirPath(), consts.WritableFsName)
}

// Path to the file where the snapshot memory file is store on host.
func (t *VmTemplate) EnvMemfilePath() string {
	return filepath.Join(t.EnvDirPath(), consts.MemfileName)
}

// Path to the file where the snapshot metadata is store on host.
func (t *VmTemplate) EnvSnapfilePath() string {
	return filepath.Join(t.EnvDirPath(), consts.SnapfileName)
}

// Path to the directory where contains the rootfs file.
// It used as a temporary path. The difference between with envRootfsPath
// is that it is actually a bind mount in standalone mount namespace.
// Thus, there can be multiple instance of tmpRunningPath (each in a
// seperate mount ns).
func (t *VmTemplate) TmpRunningPath() string {
	return filepath.Join(t.EnvDirPath(), "run")
}

// The running path where save the rootfs, see more about [VmTemplate.TmpRunningPath]
func (t *VmTemplate) TmpRootfsPath() string {
	return filepath.Join(t.TmpRunningPath(), consts.RootfsName)
}

// The running path where save the writable rootfs, see more about [VmTemplate.TmpRunningPath]
func (t *VmTemplate) TmpWritableRootfsPath() string {
	return filepath.Join(t.TmpRunningPath(), consts.WritableFsName)
}

// The dir on the host where should keep the kernel vmlinux
func (t *VmTemplate) KernelDirPath() string {
	return filepath.Join(consts.KernelsDir, t.KernelVersion)
}

func (t *VmTemplate) KernelMountDirPath() string {
	return consts.KernelMountDir
}

// The path of the kernel image path that should passed to FC
// (i.e., similar to [VmTemplate.tmpRunningPath]).
//
// As typically, kernel itself is not bound to a specific template,
// so we do not maintain kernel the same way as rootfs or snapshot file (
// i.e., store in the env dir).
func (t *VmTemplate) KernelMountPath() string {
	return filepath.Join(t.KernelMountDirPath(), consts.KernelName)
}

// The path of the template configuration file.
// It is located in [VmTemplate.EnvDirPath]
func (e *VmTemplate) TemplateFilePath() string {
	return filepath.Join(e.EnvDirPath(), consts.TemplateFileName)
}
