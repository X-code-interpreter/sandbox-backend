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
	firecracker "github.com/X-code-interpreter/sandbox-backend/packages/shared/fc"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/hypervisor"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"github.com/X-code-interpreter/sandbox-backend/packages/template-manager/constants"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func init() {
	if err := utils.CreateDirAllIfNotExists(consts.KernelMountDir); err != nil {
		err = fmt.Errorf("error make dir %s: %w", consts.KernelMountDir, err)
		panic(err)
	}
}

type vmm struct {
	hypervisor.Hypervisor
	cmd *exec.Cmd
}

type Snapshot struct {
	vmm        vmm
	env        *Env
	socketPath string
}

// This function will initialize s.client
func (s *Snapshot) startVMM(
	ctx context.Context,
	tracer trace.Tracer,
	fcBinaryPath string,
	network *network.NetworkEnvInfo,
	env *Env,
) error {
	childCtx, childSpan := tracer.Start(ctx, "start-fc-process")
	defer childSpan.End()

	if s.vmm.Hypervisor != nil || s.vmm.cmd != nil {
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
	inNetNSCmd := fmt.Sprintf("ip netns exec %s ", network.NetNsName())
	fcCmd := fmt.Sprintf(
		"%s --api-sock %s",
		fcBinaryPath,
		env.GetSocketPath(),
	)

	cmd := exec.CommandContext(childCtx, "unshare", "-pm", "--kill-child", "--", "bash", "-c", kernelMountCmd+inNetNSCmd+fcCmd)

	fcVMStdoutWriter := telemetry.NewEventWriter(childCtx, "vmm stdout")
	fcVMStderrWriter := telemetry.NewEventWriter(childCtx, "vmm stderr")

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		errMsg := fmt.Errorf("error creating fc stdout pipe: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	stderrPipe, err := cmd.StderrPipe()
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

	err = cmd.Start()
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

		waitErr := cmd.Wait()
		if err != nil {
			errMsg := fmt.Errorf("error waiting for vmm process: %w", waitErr)
			telemetry.ReportError(anonymousChildCtx, errMsg)
		} else {
			telemetry.ReportEvent(anonymousChildCtx, "vmm process exited")
		}
	}()

	// Wait for the FC process to start so we can use FC API
	client, err := firecracker.WaitForSocket(childCtx, tracer, s.socketPath, constants.SocketWaitTimeout)
	if err != nil {
		errMsg := fmt.Errorf("error waiting for vmm socket: %w", err)

		return errMsg
	}

	s.vmm.cmd = cmd
	s.vmm.Hypervisor = hypervisor.NewFirecracker(s.generateFcConfig(), client)

	telemetry.ReportEvent(childCtx, "fc process created socket")
	return nil
}

func (s *Snapshot) generateFcConfig() *hypervisor.FcConfig {
	ip := fmt.Sprintf(
		"%s::%s:%s:instance:eth0:off:8.8.8.8",
		consts.FcAddr,
		consts.FcTapAddress,
		consts.FcMaskLong,
	)

	var kernelArgs string
	// If want to check what's happening during boot
	// use the following commented kernel args
	// kernelArgs := fmt.Sprintf("quiet loglevel=6 console=ttyS0 ip=%s reboot=k panic=1 pci=off nomodules i8042.nokbd i8042.noaux ipv6.disable=1 random.trust_cpu=on overlay_root=vdb init=%s", ip, constants.OverlayInitPath)
	if s.env.Overlay {
		kernelArgs = fmt.Sprintf("quiet loglevel=1 ip=%s reboot=k panic=1 pci=off nomodules i8042.nokbd i8042.noaux ipv6.disable=1 random.trust_cpu=on overlay_root=vdb init=%s", ip, constants.OverlayInitPath)
	} else {
		kernelArgs = fmt.Sprintf("quiet loglevel=1 ip=%s reboot=k panic=1 pci=off nomodules i8042.nokbd i8042.noaux ipv6.disable=1 random.trust_cpu=on", ip)
	}
	return &hypervisor.FcConfig{
		SandboxID:          constants.SandboxIDPrefix + s.env.EnvID,
		VcpuCount:          s.env.VCpuCount,
		MemoryMB:           s.env.MemoryMB,
		KernelImagePath:    s.env.KernelMountPath(),
		KernelBootCmd:      kernelArgs,
		EnableOverlayFS:    s.env.Overlay,
		RootfsPath:         s.env.TmpRootfsPath(),
		WritableRootfsPath: s.env.TmpWritableRootfsPath(),
		FcSocketPath:       s.socketPath,
		TapDevName:         consts.FcTapName,
		GuestNetIfaceName:  consts.FcIfaceID,
		GuestNetMacAddr:    consts.FcMacAddress,
		EnableHugepage:     false,
	}
}

func (s *Snapshot) cleanupFcVM(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "cleanup-vm")
	defer childSpan.End()
	if s.vmm.cmd != nil {
		err := s.vmm.cmd.Cancel()
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

func NewSnapshot(
	ctx context.Context,
	tracer trace.Tracer,
	env *Env,
	network *network.NetworkEnvInfo,
	rootfs *Rootfs,
) (*Snapshot, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-snapshot")
	defer childSpan.End()

	fcSocketPath := env.GetSocketPath()
	snapshot := &Snapshot{
		env:        env,
		socketPath: fcSocketPath,
	}

	err := snapshot.startVMM(
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

	if err := func() error {
		ctx, span := tracer.Start(childCtx, "configure-vm")
		defer span.End()
		return snapshot.vmm.Configure(ctx)
	}(); err != nil {
		return nil, err
	}

	if err := func() error {
		ctx, span := tracer.Start(childCtx, "start-vm")
		defer span.End()
		return snapshot.vmm.Start(ctx)
	}(); err != nil {
		return nil, err
	}
	// Wait for all necessary things in FC to start
	// TODO: Maybe init should signalize when it's ready?
	time.Sleep(constants.WaitTimeForFCStart)
	telemetry.ReportEvent(
		childCtx,
		"waited for fc to start",
		attribute.Float64("seconds",
			float64(constants.WaitTimeForFCStart/time.Second)),
	)

	if env.StartCmd != "" {
		time.Sleep(constants.WaitTimeForStartCmd)
		telemetry.ReportEvent(
			childCtx,
			"waited for start command",
			attribute.Float64("seconds", float64(constants.WaitTimeForStartCmd/time.Second)),
		)
	}

	err = snapshot.vmm.Pause(childCtx)
	if err != nil {
		errMsg := fmt.Errorf("error pausing fc: %w", err)

		return nil, errMsg
	}

	{
		ctx, span := tracer.Start(childCtx, "snapshot-vm")
		err = snapshot.vmm.Snapshot(ctx, env.RunningPath())
		span.End()
		if err != nil {
			errMsg := fmt.Errorf("error snapshotting fc: %w", err)

			return nil, errMsg
		}
	}

	return snapshot, nil
}
