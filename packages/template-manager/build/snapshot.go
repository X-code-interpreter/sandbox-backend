package build

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/client"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/client/operations"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/models"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	waitTimeForFCStart  = 10 * time.Second
	waitTimeForStartCmd = 15 * time.Second
	waitTimeForFCConfig = 500 * time.Millisecond

	socketWaitTimeout = 2 * time.Second
)

type Snapshot struct {
	fc     *exec.Cmd
	client *client.FirecrackerAPI

	env        *Env
	socketPath string
}

func (s *Snapshot) startFcVM(
	ctx context.Context,
	tracer trace.Tracer,
	fcBinaryPath string,
	network *FcNetwork,
	env *Env,
) error {
	childCtx, childSpan := tracer.Start(ctx, "start-fc-process")
	defer childSpan.End()

	if s.fc != nil {
		err := fmt.Errorf("already start fc in snapshot")
		telemetry.ReportError(childCtx, err)
		return err
	}

	kernelDirOnHost := env.KernelDirPath()
	kernelDirOnVM := consts.KernelMountDir

	kernelMountCmd := fmt.Sprintf(
		"mount --bind %s %s && ",
		kernelDirOnHost,
		kernelDirOnVM,
	)
	inNetNSCmd := fmt.Sprintf("ip netns exec %s ", network.namespaceID)
	fcCmd := fmt.Sprintf(
		"%s --api-sock %s",
		fcBinaryPath,
		env.getSocketPath(),
	)

	s.fc = exec.CommandContext(childCtx, "unshare", "-pm", "--kill-child", "--", "bash", "-c", kernelMountCmd+inNetNSCmd+fcCmd)

	fcVMStdoutWriter := telemetry.NewEventWriter(childCtx, "vmm stdout")
	fcVMStderrWriter := telemetry.NewEventWriter(childCtx, "vmm stderr")

	stdoutPipe, err := s.fc.StdoutPipe()
	if err != nil {
		errMsg := fmt.Errorf("error creating fc stdout pipe: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	stderrPipe, err := s.fc.StderrPipe()
	if err != nil {
		errMsg := fmt.Errorf("error creating fc stderr pipe: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		closeErr := stdoutPipe.Close()
		if closeErr != nil {
			closeErrMsg := fmt.Errorf("error closing fc stdout pipe: %w", closeErr)
			telemetry.ReportError(childCtx, closeErrMsg)
		}

		return errMsg
	}

	var outputWaitGroup sync.WaitGroup
	outputWaitGroup.Add(1)
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)

		for scanner.Scan() {
			line := scanner.Text()
			fcVMStdoutWriter.Write([]byte(line))
		}

		outputWaitGroup.Done()
	}()

	outputWaitGroup.Add(1)
	go func() {
		scanner := bufio.NewScanner(stderrPipe)

		for scanner.Scan() {
			line := scanner.Text()
			fcVMStderrWriter.Write([]byte(line))
		}

		outputWaitGroup.Done()
	}()

	err = s.fc.Start()
	if err != nil {
		errMsg := fmt.Errorf("error starting fc process: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "started fc process")

	go func() {
		anonymousChildCtx, anonymousChildSpan := tracer.Start(ctx, "handle-fc-process-wait")
		defer anonymousChildSpan.End()

		outputWaitGroup.Wait()

		waitErr := s.fc.Wait()
		if err != nil {
			errMsg := fmt.Errorf("error waiting for fc process: %w", waitErr)
			telemetry.ReportError(anonymousChildCtx, errMsg)
		} else {
			telemetry.ReportEvent(anonymousChildCtx, "fc process exited")
		}
	}()

	// Wait for the FC process to start so we can use FC API
	err = client.WaitForSocket(childCtx, tracer, s.socketPath, socketWaitTimeout)
	if err != nil {
		errMsg := fmt.Errorf("error waiting for fc socket: %w", err)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "fc process created socket")
	return nil
}

// 1. setup boot args (including ip=xxx)
// 2. setup drivers (rootfs.ext4)
// 3. setup network interface (tap device)
// 4. setup mmds service (but we do not need populate any metadata for now)
// 5. machine config (including vpu, mem)
// 6. finally start vm
func (s *Snapshot) configureFcVM(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "configure-fc-vm")
	defer childSpan.End()

	ip := fmt.Sprintf(
		"%s::%s:%s:instance:eth0:off:8.8.8.8",
		consts.FcAddr,
		consts.FcTapAddress,
		consts.FcMaskLong,
	)
	kernelArgs := fmt.Sprintf("quiet loglevel=1 ip=%s reboot=k panic=1 pci=off nomodules i8042.nokbd i8042.noaux ipv6.disable=1 random.trust_cpu=on", ip)
	kernelImagePath := s.env.KernelMountPath()
	bootSourceConfig := operations.PutGuestBootSourceParams{
		Context: childCtx,
		Body: &models.BootSource{
			BootArgs:        kernelArgs,
			KernelImagePath: &kernelImagePath,
		},
	}

	_, err := s.client.Operations.PutGuestBootSource(&bootSourceConfig)
	if err != nil {
		errMsg := fmt.Errorf("error setting fc boot source config: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "set fc boot source config")

	rootfs := "rootfs"
	ioEngine := "Async"
	isRootDevice := true
	isReadOnly := false
	pathOnHost := s.env.tmpRootfsPath()
	driversConfig := operations.PutGuestDriveByIDParams{
		Context: childCtx,
		DriveID: rootfs,
		Body: &models.Drive{
			DriveID:      &rootfs,
			PathOnHost:   pathOnHost,
			IsRootDevice: &isRootDevice,
			IsReadOnly:   isReadOnly,
			IoEngine:     &ioEngine,
		},
	}

	_, err = s.client.Operations.PutGuestDriveByID(&driversConfig)
	if err != nil {
		errMsg := fmt.Errorf("error setting fc drivers config: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "set fc drivers config")

	ifaceID := consts.FcIfaceID
	hostDevName := consts.FcTapName
	networkConfig := operations.PutGuestNetworkInterfaceByIDParams{
		Context: childCtx,
		IfaceID: ifaceID,
		Body: &models.NetworkInterface{
			IfaceID:     &ifaceID,
			GuestMac:    consts.FcMacAddress,
			HostDevName: &hostDevName,
		},
	}

	_, err = s.client.Operations.PutGuestNetworkInterfaceByID(&networkConfig)
	if err != nil {
		errMsg := fmt.Errorf("error setting fc network config: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "set fc network config")

	smt := true
	trackDirtyPages := false

	machineConfig := &models.MachineConfiguration{
		VcpuCount:       &s.env.VCpuCount,
		MemSizeMib:      &s.env.MemoryMB,
		Smt:             &smt,
		TrackDirtyPages: &trackDirtyPages,
	}

	if s.env.HugePages {
		machineConfig.HugePages = models.MachineConfigurationHugePagesNr2M
	}

	machineConfigParams := operations.PutMachineConfigurationParams{
		Context: childCtx,
		Body:    machineConfig,
	}

	_, err = s.client.Operations.PutMachineConfiguration(&machineConfigParams)
	if err != nil {
		errMsg := fmt.Errorf("error setting fc machine config: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "set fc machine config")

	mmdsVersion := "V2"
	mmdsConfig := operations.PutMmdsConfigParams{
		Context: childCtx,
		Body: &models.MmdsConfig{
			Version:           &mmdsVersion,
			NetworkInterfaces: []string{consts.FcIfaceID},
		},
	}

	_, err = s.client.Operations.PutMmdsConfig(&mmdsConfig)
	if err != nil {
		errMsg := fmt.Errorf("error setting fc mmds config: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "set fc mmds config")

	// We may need to sleep before start - previous configuration is processes asynchronously. How to do this sync or in one go?
	time.Sleep(waitTimeForFCConfig)

	start := models.InstanceActionInfoActionTypeInstanceStart
	startActionParams := operations.CreateSyncActionParams{
		Context: childCtx,
		Info: &models.InstanceActionInfo{
			ActionType: &start,
		},
	}

	_, err = s.client.Operations.CreateSyncAction(&startActionParams)
	if err != nil {
		errMsg := fmt.Errorf("error starting fc: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "started fc")

	return nil
}

func (s *Snapshot) cleanupFcVM(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "cleanup-vm")
	defer childSpan.End()
	if s.fc != nil {
		err := s.fc.Cancel()
		if err != nil {
			errMsg := fmt.Errorf("error killing fc process: %w", err)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "killed fc process")
		}
	}

	err := os.RemoveAll(s.socketPath)
	if err != nil {
		errMsg := fmt.Errorf("error removing fc socket %w", err)
		telemetry.ReportError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed fc socket")
	}
	return nil
}

func (s *Snapshot) pauseVM(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "pause-vm")
	defer childSpan.End()
	state := models.VMStatePaused
	pauseConfig := operations.PatchVMParams{
		Context: childCtx,
		Body: &models.VM{
			State: &state,
		},
	}

	_, err := s.client.Operations.PatchVM(&pauseConfig)
	if err != nil {
		errMsg := fmt.Errorf("error pausing vm: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "paused fc")

	return nil
}

func (s *Snapshot) createSnapshot(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "create-vm-snapshot")
	defer childSpan.End()
	memfilePath := s.env.tmpMemfilePath()
	snapfileName := s.env.tmpSnapfilePath()

	params := operations.CreateSnapshotParams{
		Context: childCtx,
		Body: &models.SnapshotCreateParams{
			MemFilePath:  &memfilePath,
			SnapshotPath: &snapfileName,
			SnapshotType: models.SnapshotCreateParamsSnapshotTypeFull,
		},
	}

	_, err := s.client.Operations.CreateSnapshot(&params)
	if err != nil {
		errMsg := fmt.Errorf("error creating vm snapshot: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "created vm snapshot")

	return nil
}

func NewSnapshot(ctx context.Context, tracer trace.Tracer, env *Env, network *FcNetwork, rootfs *Rootfs) (*Snapshot, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-snapshot")
	defer childSpan.End()

	fcSocketPath := env.getSocketPath()
	client := client.NewFirecrackerAPI(fcSocketPath)

	snapshot := &Snapshot{
		socketPath: fcSocketPath,
		client:     client,
		env:        env,
		fc:         nil,
	}

	err := snapshot.startFcVM(
		childCtx,
		tracer,
		env.FirecrackerBinaryPath,
		network,
		env,
	)
	if err != nil {
		errMsg := fmt.Errorf("error starting fc process: %w", err)

		return nil, errMsg
	}
	telemetry.ReportEvent(childCtx, "started fc process")

	defer snapshot.cleanupFcVM(childCtx, tracer)

	err = snapshot.configureFcVM(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error configure fc vm: %w", err)

		return nil, errMsg
	}
	// Wait for all necessary things in FC to start
	// TODO: Maybe init should signalize when it's ready?
	time.Sleep(waitTimeForFCStart)
	telemetry.ReportEvent(childCtx, "waited for fc to start", attribute.Float64("seconds", float64(waitTimeForFCStart/time.Second)))

	if env.StartCmd != "" {
		time.Sleep(waitTimeForStartCmd)
		telemetry.ReportEvent(childCtx, "waited for start command", attribute.Float64("seconds", float64(waitTimeForStartCmd/time.Second)))
	}

	err = snapshot.pauseVM(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error pausing fc: %w", err)

		return nil, errMsg
	}

	err = snapshot.createSnapshot(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error snapshotting fc: %w", err)

		return nil, errMsg
	}

	return snapshot, nil
}
