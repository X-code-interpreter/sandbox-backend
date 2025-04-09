package sandbox

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	firecracker "github.com/X-code-interpreter/sandbox-backend/packages/shared/fc"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/hypervisor"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type vmm struct {
	hypervisor.Hypervisor
	cmd *exec.Cmd
}

func newVmm(
	ctx context.Context,
	tracer trace.Tracer,
	sandboxID string,
	enableDiffSnapshot bool,
	env *SandboxFiles,
	netEnvInfo *network.NetworkEnvInfo,
	traceID string,
) (vmm, error) {
	var vmm vmm

	childCtx, childSpan := tracer.Start(ctx, "new-fc-vm")
	defer childSpan.End()

	vmmCtx, _ := tracer.Start(
		trace.ContextWithSpanContext(context.Background(), childSpan.SpanContext()),
		"fc-vmm",
	)

	// we bind mount the EnvInstancePath (where contains the rootfs)
	// to the running path (where snapshotting happend)
	rootfsMountCmd := fmt.Sprintf(
		"mount --bind %s %s && ",
		env.EnvInstancePath(),
		env.RunningPath(),
	)

	// NOTE(huang-jl): we should not use env.KernelMountPath here
	// as it points to a file (e.g., /path/to/vmlinux), instead of a directory
	kernelMountCmd := fmt.Sprintf(
		"mount --bind %s %s && ",
		env.KernelDirPath(),
		env.KernelMountDirPath(),
	)

	inNetNSCmd := fmt.Sprintf("ip netns exec %s ", netEnvInfo.NetNsName())
	fcCmd := fmt.Sprintf(
		"%s --api-sock %s",
		env.FirecrackerBinaryPath,
		env.SocketPath,
	)

	cmd := exec.Command(
		"unshare",
		"-pfm",
		"--kill-child",
		"--",
		"bash",
		"-c",
		rootfsMountCmd+kernelMountCmd+inNetNSCmd+fcCmd,
	)
	cmdStdoutReader, cmdStdoutWriter := io.Pipe()
	cmdStderrReader, cmdStderrWriter := io.Pipe()

	cmd.Stderr = cmdStdoutWriter
	cmd.Stdout = cmdStderrWriter

	cgroupFd, err := syscall.Open(env.CgroupPath(), syscall.O_RDONLY, 0)
	if err != nil {
		errMsg := fmt.Errorf("open cgroup path when create new vm failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return vmm, errMsg
	}
	defer syscall.Close(cgroupFd)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:      true,
		CgroupFD:    cgroupFd,
		UseCgroupFD: true,
	}

	go utils.RedirectVmmOutput(vmmCtx, "firecracker stdout", cmdStdoutReader)
	go utils.RedirectVmmOutput(vmmCtx, "firecracker stderr", cmdStderrReader)

	err = cmd.Start()
	if err != nil {
		errMsg := fmt.Errorf("start vm failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return vmm, errMsg
	}
	telemetry.ReportEvent(childCtx, "vm started")

	fcClient, err := firecracker.WaitForSocket(childCtx, tracer, env.SocketPath, waitSocketTimeout)
	if err != nil {
		errMsg := fmt.Errorf("wait for fc socket failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return vmm, errMsg
	}
	telemetry.ReportEvent(childCtx, "fc process created socket")
	logCollectorAddr := fmt.Sprintf("http://%s:%d", netEnvInfo.VethIP(), consts.DefaultLogCollectorPort)
	fcConfig := &hypervisor.FcConfig{
		SandboxID: sandboxID,
		// VcpuCount
		// MemoryMB
		// KernelImagePath
		EnableDiffSnapshot: enableDiffSnapshot,
		// KernelBootCmd
		EnableOverlayFS: env.Overlay,
		// RootfsPath
		// WritableRootfsPath
		FcSocketPath: env.SocketPath,
		// TapDevName
		// GuestNetIfaceName
		// GuestNetMacAddr
		// EnableHugepage
		MmdsData: &hypervisor.MmdsMetadata{
			SandboxID: sandboxID,
			EnvID:     env.EnvID,
			Address:   logCollectorAddr,
			TraceID:   traceID,
		},
	}
	vmm.cmd = cmd
	vmm.Hypervisor = hypervisor.NewFirecracker(fcConfig, fcClient)
	{
		ctx, span := tracer.Start(childCtx, "restore-vm")
		err := vmm.Restore(ctx, env.EnvDirPath())
		span.End()
		if err != nil {
			vmm.stop(childCtx, tracer)
			errMsg := fmt.Errorf("failed to load snapshot: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return vmm, errMsg
		}
	}
	telemetry.ReportEvent(childCtx, "vm restored")
	return vmm, nil
}

func (vmm vmm) stop(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "stop-vmm")
	defer childSpan.End()

	err := vmm.cmd.Process.Kill()
	if err != nil {
		errMsg := fmt.Errorf("failed to send KILL to FC process: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	} else {
		telemetry.ReportEvent(childCtx, "sent KILL to FC process")
	}

	return nil
}

// This function must be called in order to recalim the
// resouce related to firecracker (e.g., the process id)
func (vmm vmm) wait() error {
	// close the vmm span
	if vmm.cmd == nil {
		return fmt.Errorf("fc has not started")
	}
	return vmm.cmd.Wait()
}

// create snaphot of the running vm
//
// @terminate: true to kill the vm, false to resume the vm after generating snapshot
func (vmm vmm) snapshot(ctx context.Context, tracer trace.Tracer, dir string) error {
	childCtx, childSpan := tracer.Start(ctx, "create-snapshot", trace.WithAttributes(
		attribute.String("instance.snapshot_dir", dir),
	))
	defer childSpan.End()

	if err := utils.CreateDirAllIfNotExists(dir); err != nil {
		errMsg := fmt.Errorf("failed to create instance snapshot directory: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	}

	{
		ctx, span := tracer.Start(childCtx, "pause-vm")
		err := vmm.Pause(ctx)
		span.End()

		if err != nil {
			return err
		}
		telemetry.ReportEvent(childCtx, "vm paused")
	}

	{
		ctx, span := tracer.Start(childCtx, "snapshot-vm")
		err := vmm.Snapshot(ctx, dir)
		span.End()
		if err != nil {
			return err
		}
	}
	telemetry.ReportEvent(childCtx, "vm snapshot created")

	return nil
}
