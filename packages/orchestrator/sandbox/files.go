package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/KarpelesLab/reflink"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/models"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
)

const (
	EnvInstancesDirName = "env-instances"

	socketWaitTimeout = 2 * time.Second

	cgroupfsPath     = "/sys/fs/cgroup"
	cgroupParentName = "code-interpreter"
)

type SandboxFiles struct {
	EnvID string
	// EnvPath (a dir) contains the rootfs files in env build (see template manager)
	EnvPath string
	// Different instance of same Env need has its own dir
	// this dir contains the acutal rootfs
	EnvInstancePath string
	// RunningPath path is the directory while generating snapshot
	// we need make the rootfs files in EnvInstancePath appear in RunningPath
	// (i.e., through bind mount)
	RunningPath string
	// The socket path for FC
	SocketPath string
	// The directory which actual contains the kernel (i.e., vmlinux)
	KernelDirPath string
	// The directory where kernel resides while generating snapshot
	// we also need make kernel in KernelDirPath appear in this KernelMountDirPath
	// (i.e., through bind mount)
	KernelMountDirPath string

	FirecrackerBinaryPath string

	CgroupPath string
}

// waitForSocket waits for the given file to exist
func waitForSocket(socketPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	ticker := time.NewTicker(10 * time.Millisecond)

	defer func() {
		cancel()
		ticker.Stop()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := os.Stat(socketPath); err != nil {
				continue
			}

			// TODO: Send test HTTP request to make sure socket is available
			return nil
		}
	}
}

func newSandboxFiles(
	ctx context.Context,
	tracer trace.Tracer,
	instanceID,
	envID,
	kernelVersion,
	kernelsDir,
	kernelMountDir,
	firecrackerBinaryPath string,
) (*SandboxFiles, error) {
	childCtx, childSpan := tracer.Start(ctx, "create-env-instance",
		trace.WithAttributes(
			attribute.String("env.id", envID),
			attribute.String("instanceId", instanceID),
		),
	)
	defer childSpan.End()

	envPath := filepath.Join(consts.EnvsDisk, envID)
	envInstancePath := filepath.Join(envPath, EnvInstancesDirName, instanceID)
	// to match with the template manager tmpRunningPath()
	runningPath := filepath.Join(envPath, "run")

	// Assemble socket path
	socketPath, sockErr := getSocketPath(instanceID)
	if sockErr != nil {
		errMsg := fmt.Errorf("error getting socket path: %w", sockErr)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}

	// Create kernel path
	kernelPath := filepath.Join(kernelsDir, kernelVersion)

	cgroupPath := filepath.Join(cgroupfsPath, cgroupParentName, instanceID)

	childSpan.SetAttributes(
		attribute.String("instance.env_instance_path", envInstancePath),
		attribute.String("instance.running_path", runningPath),
		attribute.String("instance.env_path", envPath),
		attribute.String("instance.kernel.mount_path", filepath.Join(kernelMountDir, consts.KernelName)),
		attribute.String("instance.kernel.path", filepath.Join(kernelPath, consts.KernelName)),
		attribute.String("instance.firecracker.path", firecrackerBinaryPath),
		attribute.String("instance.cgroup.path", cgroupPath),
	)

	return &SandboxFiles{
		EnvID:                 envID,
		EnvInstancePath:       envInstancePath,
		EnvPath:               envPath,
		RunningPath:           runningPath,
		SocketPath:            socketPath,
		KernelDirPath:         kernelPath,
		KernelMountDirPath:    kernelMountDir,
		FirecrackerBinaryPath: firecrackerBinaryPath,
		CgroupPath:            cgroupPath,
	}, nil
}

func (env *SandboxFiles) Ensure(ctx context.Context) error {
	err := os.MkdirAll(env.EnvInstancePath, 0o777)
	if err != nil {
		errMsg := fmt.Errorf("error making env instance dir: %w", err)
		telemetry.ReportError(ctx, errMsg)
		return errMsg
	}

	err = os.MkdirAll(env.RunningPath, 0o777)
	if err != nil {
		errMsg := fmt.Errorf("error making env running dir: %w", err)
		telemetry.ReportError(ctx, errMsg)
		return errMsg
	}

	err = os.MkdirAll(env.CgroupPath, 0o755)
	if err != nil {
		errMsg := fmt.Errorf("error making cgroup: %w", err)
		telemetry.ReportError(ctx, errMsg)
		return errMsg
	}

	// NOTE(huang-jl): ext4 does not support reflink
	// so we need to use xfs
	err = reflink.Always(
		filepath.Join(env.EnvPath, consts.RootfsName),
		filepath.Join(env.EnvInstancePath, consts.RootfsName),
	)
	if err != nil {
		errMsg := fmt.Errorf("error creating reflinked rootfs: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return errMsg
	}

	return nil
}

func (env *SandboxFiles) Cleanup(
	ctx context.Context,
	tracer trace.Tracer,
) error {
	childCtx, childSpan := tracer.Start(ctx, "cleanup-env-instance",
		trace.WithAttributes(
			attribute.String("instance.env_instance_path", env.EnvInstancePath),
			attribute.String("instance.running_path", env.RunningPath),
			attribute.String("instance.env_path", env.EnvPath),
		),
	)
	defer childSpan.End()
	var finalErr error

	err := os.RemoveAll(env.EnvInstancePath)
	if err != nil {
		errMsg := fmt.Errorf("error deleting env instance files: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		// TODO: Check the socket?
		telemetry.ReportEvent(childCtx, "removed all env instance files")
	}

	// Remove socket
	err = os.Remove(env.SocketPath)
	if err != nil {
		errMsg := fmt.Errorf("error deleting socket: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed socket")
	}

	err = os.RemoveAll(env.CgroupPath)
	if err != nil {
		errMsg := fmt.Errorf("error remove cgroup path: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed socket")
	}

	return finalErr
}

func (env *SandboxFiles) getSnapshotLoadParams() models.SnapshotLoadParams {
	membackendType := models.MemoryBackendBackendTypeFile
	membackendPath := env.getMemfilePath()
	snapshotPath := env.getSnapshotPath()
	return models.SnapshotLoadParams{
		MemBackend: &models.MemoryBackend{
			BackendPath: &membackendPath,
			BackendType: &membackendType,
		},
		SnapshotPath:        &snapshotPath,
		ResumeVM:            true,
		EnableDiffSnapshots: false,
	}
}

func (env *SandboxFiles) getSnapshotPath() string {
	return filepath.Join(env.EnvPath, consts.SnapfileName)
}

func (env *SandboxFiles) getMemfilePath() string {
	return filepath.Join(env.EnvPath, consts.MemfileName)
}
