package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/KarpelesLab/reflink"
	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/config"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
)

const (
	InstancesDirName         = "instances"
	InstancesSnapshotDirName = "instances-snapshot"

	socketWaitTimeout = 2 * time.Second
)

type SandboxConfig struct {
	config.VMTemplate

	DataRoot  string
	SandboxID string
	// (e.g., code-interpreter or code-interpreter/sub-cgroup )
	CgroupName string
	// The socket path for FC
	SocketPath           string
	HypervisorBinaryPath string
	// only needed for FC
	EnableDiffSnapshot bool
	MaxInstanceLength  int
	// only used by FC
	Metadata map[string]string
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

// Different instance of same Env need has its own dir
// this dir contains the (reflink) copy of the VM instance's rootfs.
func (cfg *SandboxConfig) InstancePath() string {
	return filepath.Join(cfg.TemplateDir(cfg.DataRoot), InstancesDirName, cfg.SandboxID)
}

func (cfg *SandboxConfig) InstanceRootfsPath() string {
	return filepath.Join(cfg.InstancePath(), consts.RootfsName)
}

func (cfg *SandboxConfig) InstanceWritableRootfsPath() string {
	return filepath.Join(cfg.InstancePath(), consts.WritableFsName)
}

func (cfg *SandboxConfig) CgroupPath() string {
	return filepath.Join(consts.CgroupfsPath, cfg.CgroupName, cfg.SandboxID)
}

func (cfg *SandboxConfig) PrometheusTargetPath() string {
	return filepath.Join(cfg.DataRoot, constants.PrometheusTargetsDirName, cfg.TemplateID, cfg.SandboxID+".json")
}

func (cfg *SandboxConfig) EnvInstanceCreateSnapshotPath() string {
	return filepath.Join(cfg.TemplateDir(cfg.DataRoot), InstancesSnapshotDirName, cfg.SandboxID)
}

func (cfg *SandboxConfig) EnsureFiles(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "create-sandbox-files",
		trace.WithAttributes(
			attribute.String("template_id", cfg.TemplateID),
			attribute.String("sandbox_id", cfg.SandboxID),
		),
	)
	defer childSpan.End()

	// 1. InstancePath will bind mount into PrivateDir
	// 2. Then HostKernelPath will bind mount into PrivateKernelPath
	//
	// The PrivateKernelPath is within PrivateDir.
	// To make sure that there is a mountpoint for 2, we create a file named
	// consts.KernelName in InstancePath.
	//
	// For example, the instance path is ./instance/abc, the private dir is ./run
	// the host kernel path is /path/to/vmlinux, private kernel path is ./run/vmlinux.
	// We precreate the ./instance/abc/vmlinux, so later in step 2 we can bind mount to ./run/vmlinux
	if err := utils.CreateFileAndDirIfNotExists(
		filepath.Join(cfg.InstancePath(), consts.KernelName),
		0o644,
		0o755); err != nil {
		return fmt.Errorf("error creating kernel file: %w", err)
	}
	for _, dir := range []string{
		filepath.Dir(cfg.PrometheusTargetPath()),
		cfg.InstancePath(),
		cfg.CgroupPath(),
	} {
		if err := utils.CreateDirAllIfNotExists(dir, 0o755); err != nil {
			return fmt.Errorf("error making dir %s: %w", dir, err)
		}
	}

	if cfg.Overlay {
		// 1. create reflink of writable rootfs file.
		// 2. create a hard link to base read-only rootfs file.
		err := reflink.Auto(
			cfg.HostWritableRootfsPath(cfg.DataRoot),
			cfg.InstanceWritableRootfsPath(),
		)
		if err != nil {
			errMsg := fmt.Errorf("error creating writable reflinked rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
		telemetry.ReportEvent(childCtx, "reflink of writable image created")

		// build a hard link to base rootfs
		err = os.Link(
			cfg.HostRootfsPath(cfg.DataRoot),
			cfg.InstanceRootfsPath(),
		)
		if err != nil {
			errMsg := fmt.Errorf("error linking base rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
		telemetry.ReportEvent(childCtx, "hard-link of base image created")
	} else {
		err := reflink.Auto(
			cfg.HostRootfsPath(cfg.DataRoot),
			cfg.InstanceRootfsPath(),
		)
		if err != nil {
			errMsg := fmt.Errorf("error creating writable reflinked rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
		telemetry.ReportEvent(childCtx, "reflink of base rootfs created")
	}

	return nil
}

// @keepInstanceDir: if true, do not remove env_instance_path. if false, remove.
func (cfg *SandboxConfig) CleanupFiles(
	ctx context.Context,
	tracer trace.Tracer,
	keepInstanceDir bool,
) error {
	childCtx, childSpan := tracer.Start(ctx, "cleanup-env-instance",
		trace.WithAttributes(
			attribute.String("instance.instance_path", cfg.InstancePath()),
			attribute.String("instance.private_dir", cfg.PrivateDir(cfg.DataRoot)),
			attribute.String("instance.template_dir", cfg.TemplateDir(cfg.DataRoot)),
		),
	)
	defer childSpan.End()
	var finalErr error

	if !keepInstanceDir {
		err := os.RemoveAll(cfg.InstancePath())
		if err != nil {
			errMsg := fmt.Errorf("error deleting env instance files: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)
			finalErr = errors.Join(finalErr, errMsg)
		} else {
			// TODO: Check the socket?
			telemetry.ReportEvent(childCtx, "removed all env instance files")
		}
	}

	// Remove socket
	err := os.Remove(cfg.SocketPath)
	if err != nil {
		errMsg := fmt.Errorf("error deleting socket: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed socket")
	}

	err = os.Remove(cfg.PrometheusTargetPath())
	if err != nil {
		errMsg := fmt.Errorf("error prometheus target path: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed prometheus target path")
	}

	// NOTE(huang-jl): maybe process has not been clean completely by kernel, so:
	// (1) retry rm cgroup dir for 3 times
	// (2) make remove cgroup at final step.
	sleepTimes := [3]time.Duration{
		200 * time.Millisecond,
		500 * time.Millisecond,
		1500 * time.Millisecond,
	}
	for _, sleepTime := range sleepTimes {
		if err = syscall.Rmdir(cfg.CgroupPath()); err == nil {
			break
		}
		time.Sleep(sleepTime)
	}
	if err != nil {
		errMsg := fmt.Errorf("error remove cgroup path: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed cgroup path")
	}

	return finalErr
}
