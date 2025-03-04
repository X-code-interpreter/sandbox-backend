package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/KarpelesLab/reflink"
	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/models"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/template"
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

func createDirIfNotExist(path string, perm fs.FileMode) error {
	_, err := os.Stat(path)
	if err == nil {
		// already exist
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat for path (%s) failed: %w", path, err)
	}
	// not exist error
	if err := os.Mkdir(path, perm); err != nil {
		return fmt.Errorf("create dir (%s) with perm %O failed: %w", path, perm, err)
	}
	return nil
}

func init() {
	// prometheus target path
	if err := createDirIfNotExist(constants.PrometheusTargetsPath, 0o777); err != nil {
		panic(err)
	}

	// parent cgroup path
	cgroupParentPath := filepath.Join(cgroupfsPath, cgroupParentName)
	if err := createDirIfNotExist(cgroupParentPath, 0o755); err != nil {
		panic(err)
	}
	// enable all controllers in controllers into subtree_control
	b, err := os.ReadFile(filepath.Join(cgroupParentPath, "cgroup.controllers"))
	if err != nil {
		panic(fmt.Errorf("read cgroup.controllers in %s failed: %w", cgroupParentPath, err))
	}
	controllers := strings.Fields(string(b))
	for idx, c := range controllers {
		controllers[idx] = "+" + c
	}
	f, err := os.OpenFile(filepath.Join(cgroupParentPath, "cgroup.subtree_control"), os.O_WRONLY, 0)
	if err != nil {
		panic(fmt.Errorf("open cgroup.subtree_control in %s failed: %w", cgroupParentPath, err))
	}
	defer f.Close()
	enableRequest := strings.Join(controllers, " ")
	if _, err := f.WriteString(enableRequest); err != nil {
		panic(fmt.Errorf("write %s to cgroup.subtree_control in %s failed: %w", enableRequest, cgroupParentPath, err))
	}
}

// Represent a files related to a sandbox
type SandboxFiles struct {
	template.VmTemplate

	SandboxID string

	// The socket path for FC
	SocketPath string

	FirecrackerBinaryPath string
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

func NewSandboxFiles(
	ctx context.Context,
	sandboxID,
	envID,
	firecrackerBinaryPath string,
) (*SandboxFiles, error) {
	var t template.VmTemplate
	templateFilePath := filepath.Join(consts.EnvsDisk, envID, consts.TemplateFileName)
	vmTemplateFile, err := os.Open(templateFilePath)
	if err != nil {
		errMsg := fmt.Errorf("cannot open template file for envid %s: %w", envID, err)
		telemetry.ReportCriticalError(ctx, errMsg)
		return nil, errMsg
	}
	defer vmTemplateFile.Close()

	if err = json.NewDecoder(vmTemplateFile).Decode(&t); err != nil {
		errMsg := fmt.Errorf("cannot decode template file for envid %s: %w", envID, err)
		telemetry.ReportCriticalError(ctx, errMsg)
		return nil, errMsg
	}

	// Assemble socket path
	socketPath, sockErr := getSocketPath(sandboxID)
	if sockErr != nil {
		errMsg := fmt.Errorf("error getting socket path: %w", sockErr)
		telemetry.ReportCriticalError(ctx, errMsg)
		return nil, errMsg
	}

	s := &SandboxFiles{
		VmTemplate:            t,
		SandboxID:             sandboxID,
		SocketPath:            socketPath,
		FirecrackerBinaryPath: firecrackerBinaryPath,
	}

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("instance.env_instance_path", s.EnvInstancePath()),
		attribute.String("instance.running_path", s.TmpRunningPath()),
		attribute.String("instance.env_path", s.EnvDirPath()),
		attribute.String("instance.kernel.mount_path", s.KernelMountPath()),
		attribute.String("instance.kernel.path", s.KernelDirPath()),
		attribute.String("instance.firecracker.path", firecrackerBinaryPath),
		attribute.String("instance.cgroup.path", s.CgroupPath()),
	)

	return s, nil
}

// Different instance of same Env need has its own dir
// this dir contains the (reflink) copy of the VM instance's rootfs.
func (env *SandboxFiles) EnvInstancePath() string {
	return filepath.Join(env.EnvDirPath(), EnvInstancesDirName, env.SandboxID)
}

func (env *SandboxFiles) EnvInstanceRootfsPath() string {
	return filepath.Join(env.EnvInstancePath(), consts.RootfsName)
}

func (env *SandboxFiles) EnvInstanceWritableRootfsPath() string {
	return filepath.Join(env.EnvInstancePath(), consts.WritableFsName)
}

func (env *SandboxFiles) CgroupPath() string {
	return filepath.Join(cgroupfsPath, cgroupParentName, env.SandboxID)
}

func (env *SandboxFiles) PrometheusTargetPath() string {
	return filepath.Join(constants.PrometheusTargetsPath, env.SandboxID+".json")
}

func (env *SandboxFiles) Ensure(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "create-sandbox-files",
		trace.WithAttributes(
			attribute.String("env.id", env.EnvID),
			attribute.String("sandbox.id", env.SandboxID),
		),
	)
	defer childSpan.End()
	err := os.MkdirAll(env.EnvInstancePath(), 0o777)
	if err != nil {
		errMsg := fmt.Errorf("error making env instance dir: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return errMsg
	}

	telemetry.ReportEvent(childCtx, "env instance directory created")

	err = os.MkdirAll(env.TmpRunningPath(), 0o777)
	if err != nil {
		errMsg := fmt.Errorf("error making env running dir: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return errMsg
	}

	telemetry.ReportEvent(childCtx, "sandbox running directory created")

	err = os.Mkdir(env.CgroupPath(), 0o755)
	if err != nil {
		errMsg := fmt.Errorf("error making cgroup: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return errMsg
	}

	telemetry.ReportEvent(childCtx, "sandbox cgroup created")

	// NOTE(huang-jl): ext4 does not support reflink
	// so we must use xfs
	if env.Overlay {
		// 1. create reflink of writable rootfs file.
		// 2. create a hard link to base read-only rootfs file.
		err = reflink.Always(env.EnvWritableRootfsPath(), env.EnvInstanceWritableRootfsPath())
		if err != nil {
			errMsg := fmt.Errorf("error creating writable reflinked rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
		telemetry.ReportEvent(childCtx, "reflink of writable image created")

		// build a hard link to base rootfs
		err = os.Link(env.EnvRootfsPath(), env.EnvInstanceRootfsPath())
		if err != nil {
			errMsg := fmt.Errorf("error linking base rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
		telemetry.ReportEvent(childCtx, "hard-link of base image created")
	} else {
		err = reflink.Always(env.EnvRootfsPath(), env.EnvInstanceRootfsPath())
		if err != nil {
			errMsg := fmt.Errorf("error creating writable reflinked rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
		telemetry.ReportEvent(childCtx, "reflink of base rootfs created")
	}

	return nil
}

func (env *SandboxFiles) Cleanup(
	ctx context.Context,
	tracer trace.Tracer,
) error {
	childCtx, childSpan := tracer.Start(ctx, "cleanup-env-instance",
		trace.WithAttributes(
			attribute.String("instance.env_instance_path", env.EnvInstancePath()),
			attribute.String("instance.running_path", env.TmpRunningPath()),
			attribute.String("instance.env_path", env.EnvDirPath()),
		),
	)
	defer childSpan.End()
	var finalErr error

	err := os.RemoveAll(env.EnvInstancePath())
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

	// NOTE(huang-jl): maybe process has not been clean completely by kernel,
	// so retry rm cgroup dir for 3 times
	for i := 0; i < 3; i++ {
		if err := syscall.Rmdir(env.CgroupPath()); err == nil {
			break
		}
		time.Sleep(time.Duration(20*(i+1)) * time.Millisecond)
	}
	if err != nil {
		errMsg := fmt.Errorf("error remove cgroup path: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed cgroup path")
	}

	err = os.Remove(env.PrometheusTargetPath())
	if err != nil {
		errMsg := fmt.Errorf("error prometheus target path: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed prometheus target path")
	}

	return finalErr
}

func (env *SandboxFiles) getSnapshotLoadParams() models.SnapshotLoadParams {
	membackendType := models.MemoryBackendBackendTypeFile
	membackendPath := env.EnvMemfilePath()
	snapshotPath := env.EnvSnapfilePath()
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
