package build

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/operations"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	tmpSocketDir        = "/tmp"
	waitTimeForFCStart  = 10 * time.Second
	waitTimeForStartCmd = 15 * time.Second
)

type Snapshot struct {
	vm *firecracker.Machine

	env *Env
}

type fcConfigOpt func(*firecracker.Config)

func withNetwork(n *network.FCNetwork) fcConfigOpt {
	return func(cfg *firecracker.Config) {
		cfg.NetNS = n.NetNsPath()
	}
}

func withEnv(env *Env) fcConfigOpt {
	return func(cfg *firecracker.Config) {
		// rootfs drivers of FC
		rootfs := "rootfs"
		ioEngine := "Async"
		isRootDevice := true
		isReadOnly := false
		pathOnHost := env.tmpRootfsPath()
		rootfsDriver := models.Drive{
			DriveID:      &rootfs,
			IoEngine:     &ioEngine,
			IsReadOnly:   &isReadOnly,
			IsRootDevice: &isRootDevice,
			PathOnHost:   &pathOnHost,
		}
		cfg.Drives = []models.Drive{rootfsDriver}

		// api-socket of FC
		socketFileName := fmt.Sprintf("fc-sock-%s.sock", env.EnvID)
		socketPath := filepath.Join(tmpSocketDir, socketFileName)
		cfg.SocketPath = socketPath

		// kernel image path
		kernelImagePath := env.KernelMountPath()
		cfg.KernelImagePath = kernelImagePath

		smt := true
		trackDirtyPages := false // TODO(huang-jl): should we use it to do working set detection?
		machineCfg := models.MachineConfiguration{
			VcpuCount:       &env.VCpuCount,
			MemSizeMib:      &env.MemoryMB,
			Smt:             &smt,
			TrackDirtyPages: &trackDirtyPages,
		}
		cfg.MachineCfg = machineCfg
		cfg.VMID = env.tmpInstanceID()
	}
}

// NOTE the returned config has not set the net namespace
func getFCConfig(opts ...fcConfigOpt) (*firecracker.Config, error) {
	networkConfig, err := network.GetDefaultCNINetworkConfig()
	if err != nil {
		return nil, fmt.Errorf("error get default cni network config: %w", err)
	}
	// THE ip args will be set by sdk
	kernelArgs := "quiet loglevel=1 reboot=k panic=1 pci=off nomodules i8042.nokbd i8042.noaux ipv6.disable=1 random.trust_cpu=on"
	// NOTE (huang-jl): we do not need set the socketPath here
	// as we are using VmmCommandBuilder
	cfg := &firecracker.Config{
		// disable validation since kernel image not be prepared before run the cmd
		DisableValidation: true,
		KernelArgs:        kernelArgs,
		MmdsVersion:       firecracker.MMDSv2,
		NetworkInterfaces: []firecracker.NetworkInterface{networkConfig},
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg, nil
}

func startFCVM(
	ctx context.Context,
	tracer trace.Tracer,
	fcBinaryPath string,
	network *network.FCNetwork,
	env *Env,
) (*firecracker.Machine, error) {
	childCtx, childSpan := tracer.Start(ctx, "start-fc-process")
	defer childSpan.End()

	// api-socket of FC
	socketFileName := fmt.Sprintf("fc-sock-%s.sock", env.EnvID)
	socketPath := filepath.Join(tmpSocketDir, socketFileName)

	vmCfg, err := getFCConfig(withEnv(env), withNetwork(network))
	if err != nil {
		return nil, fmt.Errorf("error get fc config: %w", err)
	}

	kernelDirOnHost := env.KernelDirPath()
	kernelDirOnVM := consts.KernelMountDir
	//setup stdout and stdin
	fcVMStdoutWriter := telemetry.NewEventWriter(childCtx, "stdout")
	fcVMStderrWriter := telemetry.NewEventWriter(childCtx, "stderr")

	kernelMountCmd := fmt.Sprintf(
		"mount --bind %s %s && ",
		kernelDirOnHost,
		kernelDirOnVM,
	)
	fcCmd := fmt.Sprintf(
		"%s --api-sock %s",
		fcBinaryPath,
		socketPath,
	)

	vmBuilder := firecracker.VMCommandBuilder{}.
		WithBin("unshare").
		AddArgs("-pm", "--kill-child", "--", "bash", "-c").
		AddArgs(kernelMountCmd + fcCmd).
		WithStdout(fcVMStdoutWriter).
		WithStderr(fcVMStderrWriter)

	cmd := vmBuilder.Build(childCtx)
	telemetry.ReportEvent(childCtx, "start fc vm", attribute.String("cmd", cmd.String()))
	vm, err := firecracker.NewMachine(childCtx, *vmCfg, firecracker.WithProcessRunner(cmd))
	if err != nil {
		return nil, fmt.Errorf("error new machine: %w", err)
	}
	if err := vm.Start(childCtx); err != nil {
		return nil, fmt.Errorf("error when start vm instance: %w", err)
	}
	return vm, nil
}

func (s *Snapshot) cleanupVM(ctx context.Context, tracer trace.Tracer) error {
	_, childSpan := tracer.Start(ctx, "cleanup-vm")
	defer childSpan.End()
	err := s.vm.StopVMM()
	if err != nil {
		return fmt.Errorf("stop vmm failed: %w", err)
	}
	// wait for vm to cleanup
	// more details need to see firecracker-go-sdk
	return s.vm.Wait(ctx)
}

func (s *Snapshot) pauseVM(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "pause-vm")
	defer childSpan.End()
	return s.vm.PauseVM(childCtx)
}

func (s *Snapshot) createSnapshot(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "create-vm-snapshot")
	defer childSpan.End()
	memfilePath := s.env.tmpMemfilePath()
	snapfileName := s.env.tmpSnapfilePath()
	return s.vm.CreateSnapshot(
		childCtx,
		memfilePath,
		snapfileName,
		func(csp *operations.CreateSnapshotParams) {
			csp.Body.SnapshotType = models.SnapshotCreateParamsSnapshotTypeFull
		})
}

func NewSnapshot(ctx context.Context, tracer trace.Tracer, env *Env, network *network.FCNetwork, rootfs *Rootfs) (*Snapshot, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-snapshot")
	defer childSpan.End()

	vm, err := startFCVM(
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

	snapshot := &Snapshot{
		vm:  vm,
		env: env,
	}
	defer func() {
		if err := snapshot.cleanupVM(childCtx, tracer); err != nil {
			telemetry.ReportError(childCtx, err)
		}
	}()
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
