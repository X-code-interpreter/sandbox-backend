package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	firecracker "github.com/X-code-interpreter/sandbox-backend/packages/shared/fc"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/client"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/client/operations"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/models"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type FcVM struct {
	cmd    *exec.Cmd
	stdout *io.PipeReader
	stderr *io.PipeReader

	ctx context.Context

	metadata *MmdsMetadata

	id string

	env                *SandboxFiles
	enableDiffSnapshot bool

	fcClient *client.FirecrackerAPI
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

func newFCVM(
	ctx context.Context,
	tracer trace.Tracer,
	sandboxID string,
	enableDiffSnapshot bool,
	env *SandboxFiles,
	fcNet *FcNetwork,
	traceID string,
) (*FcVM, error) {
	_, childSpan := tracer.Start(ctx, "new-fc-vm")
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
		env.TmpRunningPath(),
	)

	// NOTE(huang-jl): we should not use env.KernelMountPath here
	// as it points to a file (e.g., /path/to/vmlinux), instead of a directory
	kernelMountCmd := fmt.Sprintf(
		"mount --bind %s %s && ",
		env.KernelDirPath(),
		env.KernelMountDirPath(),
	)

	inNetNSCmd := fmt.Sprintf("ip netns exec %s ", fcNet.NetNsName())
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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		// NOTE(huang-jl): do not forget set CgroupFD latter in [FcVm.startVM]
		// for example:
		// CgroupFD:    cgroupFd,
		UseCgroupFD: true,
	}
	cmdStdoutReader, cmdStdoutWriter := io.Pipe()
	cmdStderrReader, cmdStderrWriter := io.Pipe()

	cmd.Stderr = cmdStdoutWriter
	cmd.Stdout = cmdStderrWriter

	logCollectorAddr := fmt.Sprintf("http://%s:%d", fcNet.VethIP(), consts.DefaultLogCollectorPort)

	vm := &FcVM{
		cmd:                cmd,
		stdout:             cmdStdoutReader,
		stderr:             cmdStderrReader,
		ctx:                vmmCtx,
		id:                 sandboxID,
		enableDiffSnapshot: enableDiffSnapshot,
		env:                env,
		metadata: &MmdsMetadata{
			SandboxID: sandboxID,
			EnvID:     env.EnvID,
			Address:   logCollectorAddr,
			TraceID:   traceID,
		},
	}

	return vm, nil
}

func (fc *FcVM) redirectStdout() {
	defer func() {
		readerErr := fc.stdout.Close()
		if readerErr != nil {
			errMsg := fmt.Errorf("error closing vmm stdout reader: %w", readerErr)
			telemetry.ReportError(fc.ctx, errMsg)
		}
	}()

	scanner := bufio.NewScanner(fc.stdout)

	for scanner.Scan() {
		line := scanner.Text()

		telemetry.ReportEvent(fc.ctx, "vmm log",
			attribute.String("type", "stdout"),
			attribute.String("message", line),
		)
		fmt.Printf("[firecracker stdout]: %s — %s\n", fc.id, line)
	}

	readerErr := scanner.Err()
	if readerErr != nil {
		errMsg := fmt.Errorf("error reading vmm stdout: %w", readerErr)
		telemetry.ReportError(fc.ctx, errMsg)
		fmt.Printf("[firecracker stdout error]: %s — %v\n", fc.id, errMsg)
	} else {
		telemetry.ReportEvent(fc.ctx, "vmm stdout reader closed")
	}

	defer func() {
		readerErr := fc.stderr.Close()
		if readerErr != nil {
			errMsg := fmt.Errorf("error closing vmm stdout reader: %w", readerErr)
			telemetry.ReportError(fc.ctx, errMsg)
		}
	}()

}

func (fc *FcVM) redirectStderr() {
	defer func() {
		readerErr := fc.stderr.Close()
		if readerErr != nil {
			errMsg := fmt.Errorf("error closing vmm stdout reader: %w", readerErr)
			telemetry.ReportError(fc.ctx, errMsg)
		}
	}()
	scanner := bufio.NewScanner(fc.stderr)

	for scanner.Scan() {
		line := scanner.Text()

		telemetry.ReportEvent(fc.ctx, "vmm log",
			attribute.String("type", "stdout"),
			attribute.String("message", line),
		)
		fmt.Printf("[firecracker stderr]: %s — %s\n", fc.id, line)
	}

	readerErr := scanner.Err()
	if readerErr != nil {
		errMsg := fmt.Errorf("error reading vmm stdout: %w", readerErr)
		telemetry.ReportError(fc.ctx, errMsg)
		fmt.Printf("[firecracker stderr error]: %s — %v\n", fc.id, errMsg)
	} else {
		telemetry.ReportEvent(fc.ctx, "vmm stdout reader closed")
	}
}

func (fc *FcVM) startVM(
	ctx context.Context,
	tracer trace.Tracer,
) error {
	childCtx, childSpan := tracer.Start(ctx, "start-vm")
	defer childSpan.End()

	go fc.redirectStderr()
	go fc.redirectStdout()

	cgroupFd, err := syscall.Open(fc.env.CgroupPath(), syscall.O_RDONLY, 0)
	if err != nil {
		errMsg := fmt.Errorf("open cgroup path when create new vm failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	}
	fc.cmd.SysProcAttr.CgroupFD = cgroupFd
	defer func() {
		if err := syscall.Close(cgroupFd); err != nil {
			errMsg := fmt.Errorf("close cgroup fd failed: %w", err)
			telemetry.ReportError(childCtx, errMsg)
		}
	}()

	err = fc.cmd.Start()
	if err != nil {
		errMsg := fmt.Errorf("start vm failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	}
	telemetry.ReportEvent(childCtx, "vm started")

	fcClient, err := firecracker.WaitForSocket(childCtx, tracer, fc.env.SocketPath, waitSocketTimeout)
	if err != nil {
		errMsg := fmt.Errorf("wait for fc socket failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	}
	telemetry.ReportEvent(childCtx, "fc process created socket")

	fc.fcClient = fcClient
	err = fc.loadSnapshot(childCtx, tracer)
	if err != nil {
		fc.stopVM(childCtx, tracer)

		errMsg := fmt.Errorf("failed to load snapshot: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "fc loaded snapshot",
		attribute.String("sanbodx.id", fc.id),
		attribute.String("sanbodx.env.id", fc.env.EnvID),
		attribute.String("sanbodx.env.path", fc.env.EnvDirPath()),
		attribute.String("sanbodx.instance.path", fc.env.EnvInstancePath()),
		attribute.String("sanbodx.socket.path", fc.env.SocketPath),
	)

	return nil
}

func retryHttpRequest(ctx context.Context, httpReqFunc func() error, maxRetryTimes int) (int, error) {
	var err error
	retryTimes := 0
	retryInterval := 50 * time.Millisecond
	timer := time.NewTimer(retryInterval)
	// we will retry when encounter errors
	for {
		err = httpReqFunc()
		if err == nil {
			return retryTimes, nil
		}
		// we only retry with EOF error
		if e := (&url.Error{}); errors.As(err, &e) {
			if errors.Is(e.Err, io.EOF) {
				goto cont
			}
		}
		return retryTimes, err
		// first check context
	cont:
		select {
		case <-ctx.Done():
			return retryTimes, ctx.Err()
		case <-timer.C:
			if retryInterval < time.Second {
				retryInterval *= 2
			}
			timer.Reset(retryInterval)
		}
		retryTimes += 1
		if retryTimes > maxRetryTimes {
			return retryTimes, fmt.Errorf("reach max retry times, last error: %w", err)
		}
	}
}

func (fc *FcVM) loadSnapshot(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "load-snapshot", trace.WithAttributes(
		attribute.String("instance.socket.path", fc.env.SocketPath),
		attribute.String("instance.snapshot.root_path", fc.env.EnvDirPath()),
	))
	defer childSpan.End()
	if fc.fcClient == nil {
		return fmt.Errorf("fc client has not been initialized, call WaitForSocket() first")
	}

	snapshotLoadParams := fc.getSnapshotLoadParams()
	snapshotConfig := operations.LoadSnapshotParams{
		Context: childCtx,
		Body:    &snapshotLoadParams,
	}

	// retry for 3 times
	retryTimes, err := retryHttpRequest(childCtx, func() error {
		_, err := fc.fcClient.Operations.LoadSnapshot(&snapshotConfig)
		return err
	}, 3)
	if err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}
	telemetry.ReportEvent(childCtx, "snapshot loaded", attribute.Int("retry_times", retryTimes))

	mmdsConfig := operations.PutMmdsParams{
		Context: childCtx,
		Body:    fc.metadata,
	}

	// retry for 3 times
	retryTimes, err = retryHttpRequest(childCtx, func() error {
		_, err = fc.fcClient.Operations.PutMmds(&mmdsConfig)
		return err
	}, 3)
	if err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}

	telemetry.ReportEvent(childCtx, "mmds data set", attribute.Int("retry_times", retryTimes))

	return nil
}

func (fc *FcVM) stopVM(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "stop-fc", trace.WithAttributes(
		attribute.String("sandbox.id", fc.id),
	))
	defer childSpan.End()

	err := fc.cmd.Process.Kill()
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
func (fc *FcVM) wait() error {
	// close the vmm span
	span := trace.SpanFromContext(fc.ctx)
	defer span.End()
	if fc.cmd == nil {
		return fmt.Errorf("fc has not started")
	}
	return fc.cmd.Wait()
}

// create snaphot of the running vm
//
// @terminate: true to kill the vm, false to resume the vm after generating snapshot
func (fc *FcVM) snapshot(ctx context.Context, tracer trace.Tracer) error {
	snapshotDir := fc.env.EnvInstanceCreateSnapshotPath()
	childCtx, childSpan := tracer.Start(ctx, "create-snapshot", trace.WithAttributes(
		attribute.String("instance.snapshot_dir", snapshotDir),
	))
	defer childSpan.End()

	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		errMsg := fmt.Errorf("failed to create instance snapshot directory: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	}

	if err := fc.pause(childCtx, tracer); err != nil {
		// no need to report error again
		return err
	}
	createSnapshotParam := fc.getCreateSnapshotParams()
	params := operations.CreateSnapshotParams{
		Context: childCtx,
		Body:    &createSnapshotParam,
	}
	_, err := fc.fcClient.Operations.CreateSnapshot(&params)
	if err != nil {
		errMsg := fmt.Errorf("error creating vm snapshot: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "created vm snapshot")

	return nil
}

func (fc *FcVM) pause(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "pause-vm")
	defer childSpan.End()
	state := models.VMStatePaused
	pauseConfig := operations.PatchVMParams{
		Context: childCtx,
		Body: &models.VM{
			State: &state,
		},
	}

	_, err := fc.fcClient.Operations.PatchVM(&pauseConfig)
	if err != nil {
		errMsg := fmt.Errorf("error pausing vm: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "fc vm paused")

	return nil
}

func (fc *FcVM) resume(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "resume-vm")
	defer childSpan.End()
	state := models.VMStateResumed
	resumeConfig := operations.PatchVMParams{
		Context: childCtx,
		Body: &models.VM{
			State: &state,
		},
	}

	_, err := fc.fcClient.Operations.PatchVM(&resumeConfig)
	if err != nil {
		errMsg := fmt.Errorf("error resuming vm: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "fc vm resumed")

	return nil
}

func (fc *FcVM) getSnapshotLoadParams() models.SnapshotLoadParams {
	membackendType := models.MemoryBackendBackendTypeFile
	membackendPath := fc.env.EnvMemfilePath()
	snapshotPath := fc.env.EnvSnapfilePath()
	return models.SnapshotLoadParams{
		MemBackend: &models.MemoryBackend{
			BackendPath: &membackendPath,
			BackendType: &membackendType,
		},
		SnapshotPath:        &snapshotPath,
		ResumeVM:            true,
		EnableDiffSnapshots: fc.enableDiffSnapshot,
	}
}

func (fc *FcVM) getCreateSnapshotParams() models.SnapshotCreateParams {
	snapshotType := models.SnapshotCreateParamsSnapshotTypeFull
	if fc.enableDiffSnapshot {
		snapshotType = models.SnapshotCreateParamsSnapshotTypeDiff
	}

	memfilePath := fc.env.EnvInstanceCreateSnapshotMemfilePath()
	snapfilePath := fc.env.EnvInstanceCreateSnapshotSnapfilePath()
	return models.SnapshotCreateParams{
		MemFilePath:  &memfilePath,
		SnapshotPath: &snapfilePath,
		SnapshotType: snapshotType,
	}
}
