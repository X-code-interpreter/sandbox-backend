package build

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/ch"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	firecracker "github.com/X-code-interpreter/sandbox-backend/packages/shared/fc"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/hypervisor"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/template"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"github.com/X-code-interpreter/sandbox-backend/packages/template-manager/constants"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func init() {
	if err := utils.CreateDirAllIfNotExists(consts.KernelMountDir, 0o755); err != nil {
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
	network *network.NetworkEnvInfo,
	env *Env,
) error {
	childCtx, childSpan := tracer.Start(ctx, "start-fc-process")
	defer childSpan.End()

	if s.vmm.Hypervisor != nil || s.vmm.cmd != nil {
		err := fmt.Errorf("already start fc in snapshot")
		telemetry.ReportCriticalError(childCtx, err)
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
	var hypervisorCmd string
	switch env.VmmType {
	case template.FIRECRACKER:
		hypervisorCmd = hypervisor.FirecrackerCmd(s.env.HypervisorBinaryPath, s.socketPath)
	case template.CLOUDHYPERVISOR:
		hypervisorCmd = hypervisor.CloudHypervisorCmd(s.env.HypervisorBinaryPath, s.socketPath)
	default:
		err := template.InvalidVmmType
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}

	cmd := exec.CommandContext(
		childCtx,
		"unshare", "-pm", "--kill-child", "--",
		"bash", "-c",
		kernelMountCmd+inNetNSCmd+hypervisorCmd,
	)

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
		errMsg := fmt.Errorf("error starting vmm process: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "started vmm process", attribute.String("hypervisor_cmd", hypervisorCmd))

	go func() {
		anonymousChildCtx, anonymousChildSpan := tracer.Start(ctx, "handle-vmm-process-wait")
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

	s.vmm.cmd = cmd
	switch s.env.VmmType {
	case template.FIRECRACKER:
		// Wait for the FC process to start so we can use FC API
		client, err := firecracker.WaitForSocket(childCtx, tracer, s.socketPath, consts.WaitTimeForHypervisorSocket)
		if err != nil {
			errMsg := fmt.Errorf("error waiting for vmm socket: %w", err)

			return errMsg
		}
		s.vmm.Hypervisor = hypervisor.NewFirecracker(s.generateFcConfig(), client)
	case template.CLOUDHYPERVISOR:
		client, err := ch.WaitForSocket(childCtx, tracer, s.socketPath, consts.WaitTimeForHypervisorSocket)
		if err != nil {
			errMsg := fmt.Errorf("error waiting for vmm socket: %w", err)

			return errMsg
		}
		s.vmm.Hypervisor = hypervisor.NewCloudHypervisor(s.generateChConfig(), client)
	default:
		err := template.InvalidVmmType
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}

	telemetry.ReportEvent(childCtx, "fc process created socket")
	return nil
}

func (s *Snapshot) generateFcConfig() *hypervisor.FcConfig {
	var kernelArgs = []string{
		"reboot=k",
		"panic=1",
		"nomodules",
		"ipv6.disable=1",
		"random.trust_cpu=on",
		"pci=off",
		"i8042.nokbd i8042.noaux",
		//client-ip,server-ip,gateway-ip,netmask,hostname,device,autoconf,dns0-ip
		fmt.Sprintf("ip=%s::%s:%s:fc-instance:%s:off:8.8.8.8",
			consts.GuestNetIpAddr,
			consts.HostTapIpAddress,
			consts.GuestNetIpMaskLong,
			consts.GuestIfaceName,
		),
	}

	if s.env.KernelDebugOutput {
		kernelArgs = append(kernelArgs, "loglevel=6 console=ttyS0")
	} else {
		kernelArgs = append(kernelArgs, "loglevel=1 quiet")
	}

	// If want to check what's happening during boot
	// use the following commented kernel args
	// kernelArgs := fmt.Sprintf("quiet loglevel=6 console=ttyS0 ip=%s reboot=k panic=1 pci=off nomodules i8042.nokbd i8042.noaux ipv6.disable=1 random.trust_cpu=on overlay_root=vdb init=%s", ip, constants.OverlayInitPath)
	if s.env.Overlay {
		kernelArgs = append(kernelArgs, "overlay_root=vdb init="+constants.OverlayInitPath)
	}
	return &hypervisor.FcConfig{
		VcpuCount:          s.env.VCpuCount,
		MemoryMB:           s.env.MemoryMB,
		KernelImagePath:    s.env.KernelMountPath(),
		KernelBootCmd:      strings.Join(kernelArgs, " "),
		EnableDiffSnapshot: true,
		EnableOverlayFS:    s.env.Overlay,
		RootfsPath:         s.env.TmpRootfsPath(),
		WritableRootfsPath: s.env.TmpWritableRootfsPath(),
		TapDevName:         consts.HostTapName,
		GuestNetIfaceName:  consts.GuestIfaceName,
		GuestNetMacAddr:    consts.GuestMacAddress,
		EnableHugepage:     s.env.HugePages,
	}
}

func (s *Snapshot) generateChConfig() *hypervisor.ChConfig {
	var (
		kernelArgs = []string{
			"reboot=k",
			"nomodules",
			"ipv6.disable=1",
			"random.trust_cpu=on",
			//client-ip,server-ip,gateway-ip,netmask,hostname,device,autoconf,dns0-ip
			fmt.Sprintf("ip=%s::%s:%s:ch-instance:%s:off:8.8.8.8",
				consts.GuestNetIpAddr,
				consts.HostTapIpAddress,
				consts.GuestNetIpMaskLong,
				consts.GuestIfaceName,
			),
		}
	)
	if s.env.KernelDebugOutput {
		kernelArgs = append(kernelArgs, "loglevel=6 console=hvc0")
	} else {
		kernelArgs = append(kernelArgs, "loglevel=1 quiet panic=1")
	}
	if s.env.Overlay {
		kernelArgs = append(kernelArgs,
			"root=/dev/pmem0 ro rootflags=dax=always",
			"overlay_root=vda init="+constants.OverlayInitPath,
			// "overlay_root=pmem1 overlay_root_flags=dax=always init="+constants.OverlayInitPath,
		)
	} else {
		kernelArgs = append(kernelArgs, "root=/dev/pmem0 rw rootflags=dax=always")
	}
	return &hypervisor.ChConfig{
		VcpuCount:          s.env.VCpuCount,
		MemoryMB:           s.env.MemoryMB,
		KernelImagePath:    s.env.KernelMountPath(),
		KernelBootCmd:      strings.Join(kernelArgs, " "),
		EnableOverlayFS:    s.env.Overlay,
		RootfsPath:         s.env.TmpRootfsPath(),
		WritableRootfsPath: s.env.TmpWritableRootfsPath(),
		TapDevName:         consts.HostTapName,
		GuestNetMacAddr:    consts.GuestMacAddress,
		EnableHugepage:     s.env.HugePages,
	}
}

func (s *Snapshot) cleanupVM(ctx context.Context, tracer trace.Tracer) error {
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
) (*Snapshot, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-snapshot")
	defer childSpan.End()

	socketPath := env.GetSocketPath()
	snapshot := &Snapshot{
		env:        env,
		socketPath: socketPath,
	}
	defer snapshot.cleanupVM(childCtx, tracer)

	err := snapshot.startVMM(
		childCtx,
		tracer,
		network,
		env,
	)
	if err != nil {
		errMsg := fmt.Errorf("error starting fc process: %w", err)

		return nil, errMsg
	}
	telemetry.ReportEvent(childCtx, "started fc process")

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
	time.Sleep(constants.WaitTimeForVmStart)
	telemetry.ReportEvent(
		childCtx,
		"waited for fc to start",
		attribute.Float64("seconds",
			float64(constants.WaitTimeForVmStart/time.Second)),
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
			errMsg := fmt.Errorf("error snapshotting vmm: %w", err)

			return nil, errMsg
		}
	}

	return snapshot, nil
}
