package template

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
)

type VMMType string

const (
	FIRECRACKER     VMMType = "firecracker"
	CLOUDHYPERVISOR VMMType = "cloud-hypervisor"
)

var (
	InvalidVcpuCount  = errors.New("invalid vcpu count")
	InvalidMemSize    = errors.New("invalid memory size")
	InvalidDiskSize   = errors.New("invalid disk size")
	InvalidKernelVer  = errors.New("invalid kernel version")
	InvalidVmmType    = errors.New("invalid vmm type")
)

var VMMTypeUnmarshalErr = errors.New("invalid value for VMMType when unmashal")

// MarshalJSON marshals the enum as a quoted json string
func (t VMMType) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString(`"`)
	buffer.WriteString(string(t))
	buffer.WriteString(`"`)
	return buffer.Bytes(), nil
}

// UnmarshalJSON unmashals a quoted json string to the enum value
func (t *VMMType) UnmarshalJSON(b []byte) error {
	var j string
	err := json.Unmarshal(b, &j)
	if err != nil {
		return err
	}
	// Note that if the string cannot be found then it will be set to the zero value, 'Created' in this case.
	if j == string(FIRECRACKER) || j == string(CLOUDHYPERVISOR) {
		*t = VMMType(j)
		return nil
	}
	return fmt.Errorf("%w %s", VMMTypeUnmarshalErr, j)
}

type VmTemplate struct {
	// Unique ID of the env.
	// required
	EnvID string `json:"template"`

	// Command to run when building the env.
	// optional (default: empty)
	StartCmd string `json:"startCmd"`

	// Path to the hypervisor binary.
	// optional (default: firecracker)
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

	VmmType VMMType `json:"vmmType"`
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

// Path to the directory where contains the rootfs file.
// It used as a temporary path. The difference between with envRootfsPath
// is that it is actually a bind mount in standalone mount namespace.
// Thus, there can be multiple instance of tmpRunningPath (each in a
// seperate mount ns).
func (t *VmTemplate) RunningPath() string {
	return filepath.Join(t.EnvDirPath(), "run")
}

// The running path where save the rootfs, see more about [VmTemplate.TmpRunningPath]
func (t *VmTemplate) TmpRootfsPath() string {
	return filepath.Join(t.RunningPath(), consts.RootfsName)
}

// The running path where save the writable rootfs, see more about [VmTemplate.TmpRunningPath]
func (t *VmTemplate) TmpWritableRootfsPath() string {
	return filepath.Join(t.RunningPath(), consts.WritableFsName)
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
func (t *VmTemplate) TemplateFilePath() string {
	return filepath.Join(t.EnvDirPath(), consts.TemplateFileName)
}

func (t *VmTemplate) Validate() error {
	if t.VCpuCount == 0 {
		return InvalidVcpuCount
	}
	if t.MemoryMB == 0 {
		return InvalidMemSize
	}

	if t.DiskSizeMB == 0 {
		return InvalidDiskSize
	}

	if t.KernelVersion == "" {
		return InvalidKernelVer
	}

	switch t.VmmType {
	case FIRECRACKER:
	case CLOUDHYPERVISOR:
	default:
		return InvalidVmmType
	}
	return nil
}
