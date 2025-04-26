package hypervisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/client"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/client/operations"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/models"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
)

var (
	_ Hypervisor = (*Firecracker)(nil)
)

type FcConfig struct {
	VcpuCount          int64
	MemoryMB           int64
	KernelImagePath    string
	EnableDiffSnapshot bool
	KernelBootCmd      string
	EnableOverlayFS    bool
	RootfsPath         string
	WritableRootfsPath string
	TapDevName         string
	GuestNetIfaceName  string
	GuestNetMacAddr    string
	EnableHugepage     bool

	MmdsData *MmdsMetadata
}

func init() {
	err := utils.CreateDirAllIfNotExists(os.TempDir(), 0o777)
	if err != nil {
		panic(err)
	}
}

// The envd will use these information for logging
// for more check the envd/internal/log/exporter/mmds.go
type MmdsMetadata struct {
	SandboxID string `json:"sandboxID"`
	EnvID     string `json:"envID"`
	Address   string `json:"address"`
	TraceID   string `json:"traceID,omitempty"`
	TeamID    string `json:"teamID,omitempty"`
}

func FirecrackerCmd(binaryPath, socketPath string) string {
	return binaryPath + " --api-sock " + socketPath
}

type Firecracker struct {
	config *FcConfig
	client *client.FirecrackerAPI
}

func NewFirecracker(config *FcConfig, client *client.FirecrackerAPI) *Firecracker {
	return &Firecracker{config, client}
}

func (fc *Firecracker) configBootSource(ctx context.Context) error {
	bootSourceConfig := operations.PutGuestBootSourceParams{
		Context: ctx,
		Body: &models.BootSource{
			BootArgs:        fc.config.KernelBootCmd,
			KernelImagePath: &fc.config.KernelImagePath,
		},
	}

	telemetry.ReportEvent(ctx, "configure fc boot source", attribute.String("boot_cmd", fc.config.KernelBootCmd))

	_, err := fc.client.Operations.PutGuestBootSource(&bootSourceConfig)
	return err
}

func (fc *Firecracker) configBlkDrivers(ctx context.Context) error {
	var blkDriverConfigs []operations.PutGuestDriveByIDParams
	ioEngine := "Async"

	// first prepare the base rootfs
	{
		driverId := "rootfs"
		isRootDevice := true
		blkDriverConfigs = append(blkDriverConfigs, operations.PutGuestDriveByIDParams{
			Context: ctx,
			DriveID: driverId,
			Body: &models.Drive{
				DriveID:      &driverId,
				PathOnHost:   fc.config.RootfsPath,
				IsRootDevice: &isRootDevice,
				IsReadOnly:   fc.config.EnableOverlayFS,
				IoEngine:     &ioEngine,
			},
		})
	}

	if fc.config.EnableOverlayFS {
		driverId := "writablefs"
		isRootDevice := false
		blkDriverConfigs = append(blkDriverConfigs, operations.PutGuestDriveByIDParams{
			Context: ctx,
			DriveID: driverId,
			Body: &models.Drive{
				DriveID:      &driverId,
				PathOnHost:   fc.config.WritableRootfsPath,
				IsRootDevice: &isRootDevice,
				IsReadOnly:   false,
				IoEngine:     &ioEngine,
			},
		},
		)
	}

	for _, config := range blkDriverConfigs {
		if _, err := fc.client.Operations.PutGuestDriveByID(&config); err != nil {
			return err
		}
	}

	return nil
}

func (fc *Firecracker) configNetIf(ctx context.Context) error {
	// TODO(huang-jl): add network rate limit for each sandbox
	ifaceID := fc.config.GuestNetIfaceName
	hostDevName := fc.config.TapDevName
	networkConfig := operations.PutGuestNetworkInterfaceByIDParams{
		Context: ctx,
		IfaceID: ifaceID,
		Body: &models.NetworkInterface{
			IfaceID:     &ifaceID,
			GuestMac:    fc.config.GuestNetMacAddr,
			HostDevName: &hostDevName,
		},
	}

	_, err := fc.client.Operations.PutGuestNetworkInterfaceByID(&networkConfig)
	return err
}

func (fc *Firecracker) configMMDS(ctx context.Context) error {
	mmdsVersion := "V2"
	mmdsConfig := operations.PutMmdsConfigParams{
		Context: ctx,
		Body: &models.MmdsConfig{
			Version:           &mmdsVersion,
			NetworkInterfaces: []string{fc.config.GuestNetIfaceName},
		},
	}

	_, err := fc.client.Operations.PutMmdsConfig(&mmdsConfig)
	return err
}

func (fc *Firecracker) configMachine(ctx context.Context) error {
	smt := true
	// NOTE(by huang-jl): when generate snapshot, we track dirty pages
	// this will enables to create a Diff memory snapshot (with less disk
	// storage overhead).
	trackDirtyPages := true

	machineConfig := &models.MachineConfiguration{
		VcpuCount:       &fc.config.VcpuCount,
		MemSizeMib:      &fc.config.MemoryMB,
		Smt:             &smt,
		TrackDirtyPages: &trackDirtyPages,
	}

	if fc.config.EnableHugepage {
		machineConfig.HugePages = models.MachineConfigurationHugePagesNr2M
	}

	machineConfigParams := operations.PutMachineConfigurationParams{
		Context: ctx,
		Body:    machineConfig,
	}

	_, err := fc.client.Operations.PutMachineConfiguration(&machineConfigParams)
	return err
}

// 1. setup boot args (including ip=xxx)
// 2. setup drivers (rootfs.ext4)
// 3. setup network interface (tap device)
// 4. setup mmds service (but we do not need populate any metadata for now)
// 5. machine config (including vpu, mem)
// 6. finally start vm
func (fc *Firecracker) Configure(ctx context.Context) error {
	if err := fc.configBootSource(ctx); err != nil {
		errMsg := fmt.Errorf("error setting fc boot source config: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)
		return errMsg
	}
	telemetry.ReportEvent(ctx, "set fc boot source config")

	if err := fc.configBlkDrivers(ctx); err != nil {
		errMsg := fmt.Errorf("error setting fc drivers config: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "set fc drivers config")

	if err := fc.configNetIf(ctx); err != nil {
		errMsg := fmt.Errorf("error setting fc network config: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "set fc network config")

	if err := fc.configMachine(ctx); err != nil {
		errMsg := fmt.Errorf("error setting fc machine config: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "set fc machine config")

	if err := fc.configMMDS(ctx); err != nil {
		errMsg := fmt.Errorf("error setting fc mmds config: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "set fc mmds config")

	// We may need to sleep before start - previous configuration is processes asynchronously. How to do this sync or in one go?
	time.Sleep(consts.WaitTimeForConfig)

	return nil
}

func (fc *Firecracker) Start(ctx context.Context) error {
	// start fc
	start := models.InstanceActionInfoActionTypeInstanceStart
	startActionParams := operations.CreateSyncActionParams{
		Context: ctx,
		Info: &models.InstanceActionInfo{
			ActionType: &start,
		},
	}

	if _, err := fc.client.Operations.CreateSyncAction(&startActionParams); err != nil {
		errMsg := fmt.Errorf("error starting fc: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(ctx, "started fc")
	return nil
}

func (fc *Firecracker) Cleanup(ctx context.Context) error {
	// Do nothing
	return nil
}

func (fc *Firecracker) Pause(ctx context.Context) error {
	state := models.VMStatePaused
	pauseConfig := operations.PatchVMParams{
		Context: ctx,
		Body: &models.VM{
			State: &state,
		},
	}

	_, err := fc.client.Operations.PatchVM(&pauseConfig)
	if err != nil {
		errMsg := fmt.Errorf("error pausing vm: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(ctx, "paused fc")

	return nil
}

func (fc *Firecracker) Resume(ctx context.Context) error {
	state := models.VMStateResumed
	resumeConfig := operations.PatchVMParams{
		Context: ctx,
		Body: &models.VM{
			State: &state,
		},
	}

	_, err := fc.client.Operations.PatchVM(&resumeConfig)
	if err != nil {
		errMsg := fmt.Errorf("error resuming vm: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(ctx, "fc vm resumed")

	return nil
}

func (fc *Firecracker) Snapshot(ctx context.Context, dir string) error {
	memfilePath := filepath.Join(dir, consts.FcMemfileName)
	snapfileName := filepath.Join(dir, consts.FcSnapfileName)

	snapshotType := models.SnapshotCreateParamsSnapshotTypeFull
	if fc.config.EnableDiffSnapshot {
		snapshotType = models.SnapshotCreateParamsSnapshotTypeDiff
	}

	params := operations.CreateSnapshotParams{
		Context: ctx,
		Body: &models.SnapshotCreateParams{
			MemFilePath:  &memfilePath,
			SnapshotPath: &snapfileName,
			// SnapshotType: models.SnapshotCreateParamsSnapshotTypeFull,
			// NOTE(by huang-jl): here we generate a Diff memory snapshot
			// the memfile will only contains the written pages (i.e., it
			// generate a sparse file).
			SnapshotType: snapshotType,
		},
	}

	_, err := fc.client.Operations.CreateSnapshot(&params)
	if err != nil {
		errMsg := fmt.Errorf("error creating vm snapshot: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(ctx, "created vm snapshot")

	return nil
}

func (fc *Firecracker) Restore(ctx context.Context, dir string) error {
	memfilePath := filepath.Join(dir, consts.FcMemfileName)
	snapfileName := filepath.Join(dir, consts.FcSnapfileName)

	membackendType := models.MemoryBackendBackendTypeFile
	snapshotLoadParams := models.SnapshotLoadParams{
		MemBackend: &models.MemoryBackend{
			BackendPath: &memfilePath,
			BackendType: &membackendType,
		},
		SnapshotPath:        &snapfileName,
		ResumeVM:            true,
		EnableDiffSnapshots: fc.config.EnableDiffSnapshot,
	}
	snapshotConfig := operations.LoadSnapshotParams{
		Context: ctx,
		Body:    &snapshotLoadParams,
	}
	// retry for 3 times
	retryTimes, err := utils.RetryHttpRequest(ctx, func() error {
		_, err := fc.client.Operations.LoadSnapshot(&snapshotConfig)
		return err
	}, 3)
	if err != nil {
		telemetry.ReportCriticalError(ctx, err)
		return err
	}
	telemetry.ReportEvent(ctx, "fc snapshot loaded", attribute.Int("retry_times", retryTimes))

	mmdsConfig := operations.PutMmdsParams{
		Context: ctx,
		Body:    fc.config.MmdsData,
	}
	// retry for 3 times
	retryTimes, err = utils.RetryHttpRequest(ctx, func() error {
		_, err = fc.client.Operations.PutMmds(&mmdsConfig)
		return err
	}, 3)
	if err != nil {
		telemetry.ReportCriticalError(ctx, err, attribute.Int("retry_times", retryTimes))
		return err
	}
	telemetry.ReportEvent(ctx, "mmds data set", attribute.Int("retry_times", retryTimes))

	return nil
}
