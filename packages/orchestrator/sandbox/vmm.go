package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/ch"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/config"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	firecracker "github.com/X-code-interpreter/sandbox-backend/packages/shared/fc"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/hypervisor"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sys/unix"
)

type vmm struct {
	hypervisor.Hypervisor
	cmd *exec.Cmd
}

func newVmm(
	ctx context.Context,
	tracer trace.Tracer,
	cfg *SandboxConfig,
	net *network.SandboxNetwork,
) (vmm, error) {
	var vmm vmm

	childCtx, childSpan := tracer.Start(ctx, "new-vmm")
	defer childSpan.End()

	vmmCtx, _ := tracer.Start(
		trace.ContextWithSpanContext(context.Background(), childSpan.SpanContext()),
		"fc-vmm",
	)

	// TODO: refactor this, use unshare + mount syscall directly
	currentBinPath, err := os.Executable()
	if err != nil {
		return vmm, fmt.Errorf("error getting executable path: %w", err)
	}
	bindMountBinPath := filepath.Join(filepath.Dir(currentBinPath), "bind_mount")
	// we bind mount the EnvInstancePath (where contains the rootfs)
	// to the running path (where snapshotting happend)
	rootfsMountCmd := fmt.Sprintf(
		"%s %s %s && ",
		bindMountBinPath,
		cfg.InstancePath(),
		cfg.PrivateDir(cfg.DataRoot),
	)

	// NOTE(huang-jl): we should not use env.KernelMountPath here
	// as it points to a file (e.g., /path/to/vmlinux), instead of a directory
	kernelMountCmd := fmt.Sprintf(
		"%s %s %s && ",
		bindMountBinPath,
		cfg.HostKernelPath(cfg.DataRoot),
		cfg.PrivateKernelPath(cfg.DataRoot),
	)

	inNetNSCmd := fmt.Sprintf("ip netns exec %s ", net.NetNsName())
	var hypervisorCmd string
	switch cfg.VmmType {
	case config.FIRECRACKER:
		hypervisorCmd = hypervisor.FirecrackerCmd(cfg.HypervisorBinaryPath, cfg.SocketPath)
	case config.CLOUDHYPERVISOR:
		hypervisorCmd = hypervisor.CloudHypervisorCmd(cfg.HypervisorBinaryPath, cfg.SocketPath)
	default:
		err := config.InvalidVmmType
		telemetry.ReportCriticalError(childCtx, err)
		return vmm, err
	}

	cmd := exec.Command(
		"unshare",
		"-pfm",
		"--kill-child",
		"--",
		"bash",
		"-c",
		rootfsMountCmd+kernelMountCmd+inNetNSCmd+hypervisorCmd,
	)
	cmdStdoutReader, cmdStdoutWriter := io.Pipe()
	cmdStderrReader, cmdStderrWriter := io.Pipe()

	cmd.Stderr = cmdStdoutWriter
	cmd.Stdout = cmdStderrWriter

	cmd.SysProcAttr = &syscall.SysProcAttr{
		AmbientCaps: []uintptr{unix.CAP_SYS_ADMIN, unix.CAP_NET_ADMIN},
	}

	if constants.Repurposable {
		cgroupFd, err := syscall.Open(cfg.CgroupPath(), syscall.O_RDONLY, 0)
		if err != nil {
			errMsg := fmt.Errorf("open cgroup path when create new vm failed: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)
			return vmm, errMsg
		}
		defer syscall.Close(cgroupFd)
		// use CLONE_INTO_CGROUP
		cmd.SysProcAttr.Setsid = true
		cmd.SysProcAttr.CgroupFD = cgroupFd
		cmd.SysProcAttr.UseCgroupFD = true
	}

	go utils.RedirectVmmOutput(vmmCtx, "vmm stdout", cmdStdoutReader)
	go utils.RedirectVmmOutput(vmmCtx, "vmm stderr", cmdStderrReader)

	err = cmd.Start()
	if err != nil {
		errMsg := fmt.Errorf("start vm failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return vmm, errMsg
	}
	telemetry.ReportEvent(childCtx, "vm started")
	vmm.cmd = cmd

	if !constants.Repurposable {
		// migrate to cgroup
		if err := addProcToCgroup(cfg.CgroupPath(), cmd.Process.Pid); err != nil {
			return vmm, fmt.Errorf("migrate vmm to cgroup failed: %w", err)
		}
		telemetry.ReportEvent(childCtx, "vm miragted to cgroup")
	}

	switch cfg.VmmType {
	case config.FIRECRACKER:
		// Wait for the FC process to start so we can use FC API
		client, err := firecracker.WaitForSocket(childCtx, tracer, cfg.SocketPath, consts.WaitTimeForHypervisorSocket)
		if err != nil {
			errMsg := fmt.Errorf("error waiting for vmm socket: %w", err)

			return vmm, errMsg
		}
		telemetry.ReportEvent(childCtx, "vmm process created fc socket")
		vmm.Hypervisor = hypervisor.NewFirecracker(
			getFcConfig(cfg, net, childSpan.SpanContext().TraceID().String()),
			client,
		)
	case config.CLOUDHYPERVISOR:
		client, err := ch.WaitForSocket(childCtx, tracer, cfg.SocketPath, consts.WaitTimeForHypervisorSocket)
		if err != nil {
			errMsg := fmt.Errorf("error waiting for vmm socket: %w", err)

			return vmm, errMsg
		}
		telemetry.ReportEvent(childCtx, "vmm process created ch socket")
		vmm.Hypervisor = hypervisor.NewCloudHypervisor(getChConfig(cfg), client)
	default:
		err := config.InvalidVmmType
		telemetry.ReportCriticalError(childCtx, err)
		return vmm, err
	}

	// restore
	if err := vmm.restore(childCtx, tracer, cfg); err != nil {
		vmm.stop(childCtx, tracer)
		errMsg := fmt.Errorf("failed to restore: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
	}
	telemetry.ReportEvent(childCtx, "vm restored")
	return vmm, nil
}

func (vmm vmm) restore(ctx context.Context, tracer trace.Tracer, cfg *SandboxConfig) error {
	childCtx, childSpan := tracer.Start(ctx, "restore-vm")
	defer childSpan.End()
	if err := vmm.Restore(childCtx, cfg.TemplateImgDir(cfg.DataRoot)); err != nil {
		return err
	}
	switch cfg.VmmType {
	case config.CLOUDHYPERVISOR:
		// cloud hypervisor need explicitly resume
		if err := vmm.Resume(childCtx); err != nil {
			return err
		}
	}
	return nil
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
// resouce related to vmm (e.g., the process id)
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

	if err := utils.CreateDirAllIfNotExists(dir, 0o755); err != nil {
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

func addProcToCgroup(cgroupPath string, pid int) error {
	cgroupProcFilePath := filepath.Join(cgroupPath, "cgroup.procs")
	cgroupProcFile, err := os.OpenFile(cgroupProcFilePath, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer cgroupProcFile.Close()
	if _, err = cgroupProcFile.Write([]byte(strconv.Itoa(pid))); err != nil {
		return nil
	}
	return nil
}

func getFcConfig(cfg *SandboxConfig, net *network.SandboxNetwork, traceID string) *hypervisor.FcConfig {
	logCollectorAddr := fmt.Sprintf("http://%s:%d", net.VethIP(), consts.DefaultLogCollectorPort)
	return &hypervisor.FcConfig{
		VcpuCount:       cfg.VCpuCount,
		MemoryMB:        cfg.MemoryMB,
		KernelImagePath: cfg.PrivateKernelPath(cfg.DataRoot),
		// do not need for restore
		KernelBootCmd:      "",
		EnableDiffSnapshot: cfg.EnableDiffSnapshot,
		// do not need for restore
		EnableOverlayFS: false,
		// do not need for restore
		RootfsPath: "",
		// do not need for restore
		WritableRootfsPath: "",
		TapDevName:         consts.HostTapName,
		GuestNetIfaceName:  consts.GuestIfaceName,
		GuestNetMacAddr:    consts.GuestMacAddress,
		EnableHugepage:     cfg.HugePages,

		MmdsData: &hypervisor.MmdsMetadata{
			SandboxID: cfg.SandboxID,
			EnvID:     cfg.TemplateID,
			Address:   logCollectorAddr,
			TraceID:   traceID,
		},
	}
}

func getChConfig(cfg *SandboxConfig) *hypervisor.ChConfig {
	return &hypervisor.ChConfig{
		VcpuCount:       cfg.VCpuCount,
		MemoryMB:        cfg.MemoryMB,
		KernelImagePath: cfg.PrivateKernelPath(cfg.DataRoot),
		KernelBootCmd:   "",
		EnableOverlayFS: cfg.Overlay,
		// do not need for restore
		RootfsPath: "",
		// do not need for restore
		WritableRootfsPath: "",
		TapDevName:         consts.HostTapName,
		GuestNetMacAddr:    consts.GuestMacAddress,
		EnableHugepage:     cfg.HugePages,
	}
}
