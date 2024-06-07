package sandbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var httpClient = http.Client{
	Timeout: 5 * time.Second,
}

type Sandbox struct {
	vm      *firecracker.Machine
	env     *SandboxFiles
	config  *orchestrator.SandboxConfig
	startAt time.Time
	network *network.FCNetwork
	ipAddr  string
}

// The envd will use these information for logging
// for more check the envd/internal/log/exporter/mmds.go
type MmdsMetadata struct {
	InstanceID string `json:"instanceID"`
	EnvID      string `json:"envID"`
	Address    string `json:"address"`
	TraceID    string `json:"traceID,omitempty"`
	TeamID     string `json:"teamID,omitempty"`
}

func NewSandbox(
	ctx context.Context,
	tracer trace.Tracer,
	dns *DNS,
	config *orchestrator.SandboxConfig,
) (*Sandbox, error) {
	childCtx, childSpan := tracer.Start(
		ctx,
		"new-sandbox",
		trace.WithAttributes(attribute.String("instance.id", config.SandboxID)),
	)
	defer childSpan.End()

	// sandboxID supposed to be unique (e.g., can be generate from some random alg)
	fcNet, err := network.NewFCNetwork(childCtx, tracer, FCNetNsName(config.SandboxID))
	if err != nil {
		errMsg := fmt.Errorf("failed to create namespaces: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	defer func() {
		if err != nil {
			ntErr := fcNet.Cleanup(childCtx, tracer)
			if ntErr != nil {
				errMsg := fmt.Errorf("error removing network namespace after failed instance start: %w", ntErr)
				telemetry.ReportError(childCtx, errMsg)
			} else {
				telemetry.ReportEvent(childCtx, "removed network namespace")
			}
		}
	}()

	fsEnv, err := newSandboxFiles(
		childCtx,
		tracer,
		config.SandboxID,
		config.TemplateID,
		config.KernelVersion,
		consts.KernelsDir,
		consts.KernelMountDir,
		constants.FCBinaryPath,
	)
	if err != nil {
		errMsg := fmt.Errorf("failed to assemble env files info for FC: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	telemetry.ReportEvent(childCtx, "assembled env files info")

	err = fsEnv.Ensure(childCtx)
	if err != nil {
		errMsg := fmt.Errorf("failed to create env for FC: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	telemetry.ReportEvent(childCtx, "created env for FC")

	defer func() {
		if err != nil {
			envErr := fsEnv.Cleanup(childCtx, tracer)
			if envErr != nil {
				errMsg := fmt.Errorf("error deleting env after failed fc start: %w", err)
				telemetry.ReportCriticalError(childCtx, errMsg)
			} else {
				telemetry.ReportEvent(childCtx, "deleted env")
			}
		}
	}()

	vm, err := newFCVM(
		childCtx,
		tracer,
		config.SandboxID,
		fsEnv,
		fcNet,
	)
	if err != nil {
		errMsg := fmt.Errorf("failed to new fc vm: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}

	err = startVM(childCtx, tracer, vm, config.SandboxID, "TODO(huang-jl)", config.TemplateID)
	if err != nil {
		errMsg := fmt.Errorf("failed to start FC: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	instance := &Sandbox{
		vm:      vm,
		env:     fsEnv,
		config:  config,
		network: fcNet,
		ipAddr:  vm.Cfg.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.IPAddr.String(),
	}

	telemetry.ReportEvent(childCtx, "initialized FC", attribute.String("ip", instance.ipAddr))

	telemetry.ReportEvent(childCtx, "ensuring clock sync")

	go func() {
		backgroundCtx := context.Background()

		clockErr := instance.EnsureClockSync(backgroundCtx)
		if clockErr != nil {
			telemetry.ReportError(backgroundCtx, fmt.Errorf("failed to sync clock: %w", clockErr))
		} else {
			telemetry.ReportEvent(backgroundCtx, "clock synced")
		}
	}()

	instance.startAt = time.Now()

	return instance, nil
}

func newFCVM(
	ctx context.Context,
	tracer trace.Tracer,
	sandboxID string,
	env *SandboxFiles,
	fcNet *network.FCNetwork,
) (*firecracker.Machine, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-fc-vm")
	defer childSpan.End()

	// we bind mount the EnvInstancePath (where contains the rootfs)
	// to the running path (where snapshotting happend)
	rootfsMountCmd := fmt.Sprintf(
		"mount --bind %s %s && ",
		env.EnvInstancePath,
		env.RunningPath,
	)

	kernelMountCmd := fmt.Sprintf(
		"mount --bind %s %s && ",
		env.KernelDirPath,
		env.KernelMountDirPath,
	)

	fcCmd := fmt.Sprintf(
		"%s --api-sock %s",
		env.FirecrackerBinaryPath,
		env.SocketPath,
	)
	//setup stdout and stdin
	fcVMStdoutWriter := telemetry.NewEventWriter(childCtx, fmt.Sprintf("vmm %s stdout", sandboxID))
	fcVMStderrWriter := telemetry.NewEventWriter(childCtx, fmt.Sprintf("vmm %s stderr", sandboxID))

	networkInterface, err := network.GetDefaultCNINetworkConfig()
	if err != nil {
		errMsg := fmt.Errorf("failed to get cni network config: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}
	// disable validation since
	// 1. kernel is not prepared before kernelMountCmd
	// 2. we can skip to pass driver config as it is useless
	cfg := firecracker.Config{
		DisableValidation: true,
		LogLevel:          "Info",
		LogPath:           filepath.Join(env.EnvInstancePath, "fc.log"),
		NetworkInterfaces: []firecracker.NetworkInterface{networkInterface},
		VMID:              sandboxID,
		NetNS:             fcNet.NetNsPath(),
		Snapshot:          env.getSnapshotConfig(),
	}

	vmmBuilder := firecracker.VMCommandBuilder{}.
		WithBin("unshare").
		AddArgs("-pm", "--kill-child", "--", "bash", "-c").
		AddArgs(rootfsMountCmd + kernelMountCmd + fcCmd).
		WithStdout(fcVMStdoutWriter).
		WithStderr(fcVMStderrWriter)
	cmd := vmmBuilder.Build(childCtx)
	vm, err := firecracker.NewMachine(childCtx, cfg, firecracker.WithProcessRunner(cmd))
	if err != nil {
		errMsg := fmt.Errorf("fc sdk new machine failed: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return nil, errMsg
	}
	return vm, nil
}

func startVM(
	ctx context.Context,
	tracer trace.Tracer,
	vm *firecracker.Machine,
	sandboxID,
	logCollectorAddr,
	envID string,
) error {
	childCtx, childSpan := tracer.Start(ctx, "start-vm")
	defer childSpan.End()
	metadata := MmdsMetadata{
		InstanceID: sandboxID,
		EnvID:      envID,
		Address:    logCollectorAddr,
	}
	err := vm.Start(childCtx)
	if err != nil {
		stopVM(childCtx, tracer, vm)

		errMsg := fmt.Errorf("start vm failed: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return errMsg
	}
	defer func() {
		if err != nil {
			stopVM(childCtx, tracer, vm)
		}
	}()

	err = vm.SetMetadata(childCtx, &metadata)
	if err != nil {
		errMsg := fmt.Errorf("set mmds failed: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return errMsg
	}
	return nil
}

func stopVM(ctx context.Context, tracer trace.Tracer, vm *firecracker.Machine) {
	childCtx, childSpan := tracer.Start(ctx, "stop-fc")
	defer childSpan.End()

	err := vm.StopVMM()
	if err != nil {
		errMsg := fmt.Errorf("failed to send KILL to FC process: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "sent KILL to FC process")
	}

	return
}

func (s *Sandbox) EnsureClockSync(ctx context.Context) error {
syncLoop:
	for {
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		default:
			err := s.syncClock(ctx)
			if err != nil {
				telemetry.ReportError(ctx, fmt.Errorf("error syncing clock: %w", err))
				continue
			}
			break syncLoop
		}
	}

	return nil
}

func (s *Sandbox) syncClock(ctx context.Context) error {
	address := fmt.Sprintf("http://%s:%d/sync", s.ipAddr, consts.DefaultEnvdServerPort)

	request, err := http.NewRequestWithContext(ctx, "POST", address, nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}

	// TODO(huang-jl) why e2b do copying here?
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return err
	}

	defer response.Body.Close()

	return nil
}

func (s *Sandbox) CleanupAfterFCStop(
	ctx context.Context,
	tracer trace.Tracer,
	dns *DNS,
) {
	childCtx, childSpan := tracer.Start(ctx, "delete-instance")
	defer childSpan.End()

	err := s.network.Cleanup(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("cannot remove network when destroying task: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed network")
	}

	err = dns.Remove(s.config.SandboxID)
	if err != nil {
		errMsg := fmt.Errorf("failed to remove instance in etc hosts: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed instance in etc hosts")
	}

	err = s.env.Cleanup(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("failed to delete instance files: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "deleted instance files")
	}
}

// TODO(huang-jl): does wait need span / tracer ?
func (s *Sandbox) Wait(ctx context.Context) error {
	return s.vm.Wait(ctx)
}
