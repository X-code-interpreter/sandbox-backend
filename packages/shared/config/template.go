package config

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
	InvalidVcpuCount    = errors.New("invalid vcpu count")
	InvalidMemSize      = errors.New("invalid memory size")
	InvalidDiskSize     = errors.New("invalid disk size")
	InvalidKernelVer    = errors.New("invalid kernel version")
	InvalidVmmType      = errors.New("invalid vmm type")
	ErrVMMTypeUnmarshal = errors.New("invalid value for VMMType when unmashal")
)

func (t *VMMType) UnmarshalText(text []byte) error {
	ty := VMMType(text)
	switch ty {
	case FIRECRACKER, CLOUDHYPERVISOR:
		*t = ty
		return nil
	default:
		return fmt.Errorf("%w %s", ErrVMMTypeUnmarshal, text)
	}
}

func (t *VMMType) MarshalTOML() ([]byte, error) {
	return []byte(string(*t)), nil
}

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
	return fmt.Errorf("%w %s", ErrVMMTypeUnmarshal, j)
}

type VMTemplate struct {
	// Unique ID of the env.
	// required
	TemplateID string `toml:"template_id"`

	// Path to the hypervisor binary.
	// optional (default: firecracker)
	// The number of vCPUs to allocate to the VM.
	// required
	VCpuCount int64 `toml:"vcpu"`

	// The amount of RAM memory to allocate to the VM, in MiB.
	// required
	MemoryMB int64 `toml:"mem_mb"`

	// The amount of free disk to allocate to the VM, in MiB.
	// required
	DiskSizeMB int64 `toml:"disk_mb"`

	// Real size of the rootfs after building the env.
	RootfsSize int64 `toml:"rootfs_size"`

	// Version of the kernel.
	// optional
	KernelVersion string `toml:"kernel_version"`

	// Docker Image to used as the base image
	// if it is empty, it will be "e2bdev/code-interpreter:latest"
	// optional
	DockerImage string `toml:"docker_img"`

	// Use local docker image (i.e., do not pull from remote docker registry)
	NoPull bool `toml:"no_pull"`

	HugePages bool `toml:"huge_pages,omitempty"`

	// Create two block device for VM. One is read-only lower dir,
	// the other is writable upper dir.
	// Set this to false (by default) will create one read-write block device.
	Overlay bool `toml:"overlay"`

	VmmType VMMType `toml:"vmm_type"`

	// Command to run when building the env.
	// optional (default: empty)
	StartCmd struct {
		Cmd         string `toml:"cmd"`
		EnvFilePath string `toml:"envfile_path"`
		WorkingDir  string `toml:"working_dir"`
	} `toml:"start_cmd"`
}

// Path to the directory where the env is stored.
func (t *VMTemplate) TemplateDir(dataRoot string) string {
	return filepath.Join(dataRoot, consts.TemplateDirName, t.TemplateID)
}

func (t *VMTemplate) TemplateImgDir(dataRoot string) string {
	return filepath.Join(t.TemplateDir(dataRoot), "image")
}

// Path to the file where the rootfs is store on host.
// When enable overlay, this store the read-only lower dir of overlay root file.
func (t *VMTemplate) HostRootfsPath(dataRoot string) string {
	return filepath.Join(t.TemplateImgDir(dataRoot), consts.RootfsName)
}

// Path to the file where the writable rootfs is store on host.
// Only valid when enable overlay, this store the writable upper dir of overlay root file.
func (t *VMTemplate) HostWritableRootfsPath(dataRoot string) string {
	return filepath.Join(t.TemplateImgDir(dataRoot), consts.WritableFsName)
}

// Path to the directory where contains the rootfs file.
// It used as a temporary path. The difference compared to envRootfsPath
// is that this is actually a bind mount in standalone mount namespace.
// Thus, there can be multiple instance of tmpRunningPath (each in a
// seperate mount ns).
func (t *VMTemplate) PrivateDir(dataRoot string) string {
	return filepath.Join(t.TemplateDir(dataRoot), "run")
}

func (t *VMTemplate) PrivateRootfsPath(dataRoot string) string {
	return filepath.Join(t.PrivateDir(dataRoot), consts.RootfsName)
}

func (t *VMTemplate) PrivateWritableRootfsPath(dataRoot string) string {
	return filepath.Join(t.PrivateDir(dataRoot), consts.WritableFsName)
}

// The dir on the host where should keep the kernel vmlinux
func (t *VMTemplate) HostKernelPath(dataRoot string) string {
	return filepath.Join(dataRoot, consts.KernelDirName, t.KernelVersion, consts.KernelName)
}

func (t *VMTemplate) PrivateKernelPath(dataRoot string) string {
	return filepath.Join(t.PrivateDir(dataRoot), consts.KernelName)
}

// The path of the template configuration file.
// It is located in [VMTemplate.TemplateDir]
func (t *VMTemplate) TemplateFilePath(dataRoot string) string {
	return filepath.Join(t.TemplateDir(dataRoot), consts.TemplateFileName)
}

func (t *VMTemplate) Validate() error {
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
