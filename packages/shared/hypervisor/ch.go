package hypervisor

import (
	"context"
	"fmt"
	"os"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/ch"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"go.opentelemetry.io/otel/attribute"
)

var (
	_ Hypervisor = (*CloudHypervisor)(nil)
)

type ChConfig struct {
	VcpuCount          int64
	MemoryMB           int64
	KernelImagePath    string
	KernelBootCmd      string
	EnableOverlayFS    bool
	RootfsPath         string
	WritableRootfsPath string
	TapDevName         string
	GuestNetMacAddr    string
	EnableHugepage     bool
}

func init() {
	err := utils.CreateDirAllIfNotExists(os.TempDir(), 0o755)
	if err != nil {
		panic(err)
	}
}

type CloudHypervisor struct {
	config *ChConfig
	client *ch.ClientWithResponses
}

func CloudHypervisorCmd(binaryPath, socketPath string) string {
	return binaryPath + " --api-socket " + socketPath + " -v"
}

func NewCloudHypervisor(config *ChConfig, client *ch.ClientWithResponses) *CloudHypervisor {
	return &CloudHypervisor{config, client}
}

func (vmm *CloudHypervisor) Configure(ctx context.Context) error {
	var diskConfigs []ch.DiskConfig
	{
		id := "rootfs"
		readonly := vmm.config.EnableOverlayFS
		diskConfigs = append(diskConfigs, ch.DiskConfig{
			Id:       &id,
			Path:     vmm.config.RootfsPath,
			Readonly: &readonly,
		})
	}
	if vmm.config.EnableOverlayFS {
		id := "writablefs"
		readonly := false
		diskConfigs = append(diskConfigs, ch.DiskConfig{
			Id:       &id,
			Path:     vmm.config.RootfsPath,
			Readonly: &readonly,
		})
	}

	netConfigs := []ch.NetConfig{
		{
			Mac: &vmm.config.GuestNetMacAddr,
			Tap: &vmm.config.TapDevName,
		},
	}

	vmConfig := ch.VmConfig{
		Cpus: &ch.CpusConfig{
			BootVcpus: int(vmm.config.VcpuCount),
			MaxVcpus:  int(vmm.config.VcpuCount),
		},
		Memory: &ch.MemoryConfig{
			Size:      vmm.config.MemoryMB * 1024 * 1024,
			Hugepages: &vmm.config.EnableHugepage,
		},
		Disks: &diskConfigs,
		Net:   &netConfigs,
		Payload: ch.PayloadConfig{
			Cmdline: &vmm.config.KernelBootCmd,
			Kernel:  &vmm.config.KernelImagePath,
		},
		Console: &ch.ConsoleConfig{
			Mode: ch.ConsoleConfigModeTty,
		},
		Serial: &ch.ConsoleConfig{
			Mode: ch.ConsoleConfigModeNull,
		},
	}

	telemetry.ReportEvent(ctx, "configure ch boot source", attribute.String("boot_cmd", vmm.config.KernelBootCmd))
	if _, err := vmm.client.CreateVMWithResponse(ctx, vmConfig); err != nil {
		errMsg := fmt.Errorf("error create cloud hypervisor vm: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "created ch vm")
	return nil
}

func (vmm *CloudHypervisor) Start(ctx context.Context) error {
	if _, err := vmm.client.BootVMWithResponse(ctx); err != nil {
		errMsg := fmt.Errorf("error boot cloud hypervisor vm: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "booted ch vm")
	return nil
}

func (vmm *CloudHypervisor) Cleanup(ctx context.Context) error {
	// Do nothing
	return nil
}

func (vmm *CloudHypervisor) Pause(ctx context.Context) error {
	_, err := vmm.client.PauseVMWithResponse(ctx)
	if err != nil {
		errMsg := fmt.Errorf("error pause cloud hypervisor vm: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "paused ch vm")
	return nil
}

func (vmm *CloudHypervisor) Resume(ctx context.Context) error {
	if _, err := vmm.client.ResumeVMWithResponse(ctx); err != nil {
		errMsg := fmt.Errorf("error resume cloud hypervisor vm: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "resumed ch vm")

	return nil
}

func (vmm *CloudHypervisor) Snapshot(ctx context.Context, dir string) error {
	dest := "file://" + dir
	req := ch.VmSnapshotConfig{
		DestinationUrl: &dest,
	}
	if _, err := vmm.client.PutVmSnapshotWithResponse(ctx, req); err != nil {
		errMsg := fmt.Errorf("error snapshot cloud hypervisor vm: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "snapshotted ch vm")
	return nil
}

func (vmm *CloudHypervisor) Restore(ctx context.Context, dir string) error {
	req := ch.RestoreConfig{
		SourceUrl: "file://" + dir,
	}
	if _, err := vmm.client.PutVmRestoreWithResponse(ctx, req); err != nil {
		errMsg := fmt.Errorf("error restore cloud hypervisor vm: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	return nil
}
