package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/KarpelesLab/reflink"
	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/template"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
)

const (
	EnvInstancesDirName         = "env-instances"
	EnvInstancesSnapshotDirName = "env-instances-snapshot"

	socketWaitTimeout = 2 * time.Second
)

func init() {
	// prometheus target path
	if err := utils.CreateDirAllIfNotExists(constants.PrometheusTargetsPath, 0o755); err != nil {
		panic(err)
	}

	// parent cgroup path
	cgroupParentPath := filepath.Join(consts.CgroupfsPath, consts.CgroupParentName)
	if err := utils.CreateDirAllIfNotExists(cgroupParentPath, 0o755); err != nil {
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

type Config struct {
	template.VmTemplate

	SandboxID string
	// The socket path for FC
	SocketPath           string
	HypervisorBinaryPath string
	// only needed for FC
	EnableDiffSnapshot bool
	MaxInstanceLength  int
	Metadata           map[string]string
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

func NewSandboxConfig(
	ctx context.Context,
	req *orchestrator.SandboxCreateRequest,
) (*Config, error) {
	var t template.VmTemplate
	templateFilePath := filepath.Join(consts.EnvsDisk, req.TemplateID, consts.TemplateFileName)
	telemetry.ReportEvent(ctx, "begin create sandbox config", attribute.String("template_path", templateFilePath))
	vmTemplateFile, err := os.Open(templateFilePath)
	if err != nil {
		errMsg := fmt.Errorf("cannot open template file: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)
		return nil, errMsg
	}
	defer vmTemplateFile.Close()

	if err = json.NewDecoder(vmTemplateFile).Decode(&t); err != nil {
		errMsg := fmt.Errorf("cannot decode template file: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)
		return nil, errMsg
	}

	// Assemble socket path
	socketPath, sockErr := getSocketPath(req.SandboxID)
	if sockErr != nil {
		errMsg := fmt.Errorf("error getting socket path: %w", sockErr)
		telemetry.ReportCriticalError(ctx, errMsg)
		return nil, errMsg
	}

	var hypervisorPath string
	if req.HypervisorBinaryPath == nil || len(*req.HypervisorBinaryPath) == 0 {
		switch t.VmmType {
		case template.FIRECRACKER:
			hypervisorPath = constants.FcBinaryPath
		case template.CLOUDHYPERVISOR:
			hypervisorPath = constants.ChBinaryPath
		}
	} else {
		hypervisorPath = *req.HypervisorBinaryPath
	}

	config := &Config{
		VmTemplate:           t,
		SandboxID:            req.SandboxID,
		SocketPath:           socketPath,
		EnableDiffSnapshot:   req.EnableDiffSnapshots,
		MaxInstanceLength:    int(req.MaxInstanceLength),
		Metadata:             req.Metadata,
		HypervisorBinaryPath: hypervisorPath,
	}

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("instance.env_instance_path", config.EnvInstancePath()),
		attribute.String("instance.running_path", config.RunningPath()),
		attribute.String("instance.env_path", config.EnvDirPath()),
		attribute.String("instance.kernel.mount_path", config.KernelMountPath()),
		attribute.String("instance.kernel.path", config.KernelDirPath()),
		attribute.String("instance.hypervisor.path", config.HypervisorBinaryPath),
		attribute.String("instance.cgroup.path", config.CgroupPath()),
	)

	return config, nil
}

// Different instance of same Env need has its own dir
// this dir contains the (reflink) copy of the VM instance's rootfs.
func (config *Config) EnvInstancePath() string {
	return filepath.Join(config.EnvDirPath(), EnvInstancesDirName, config.SandboxID)
}

func (config *Config) EnvInstanceRootfsPath() string {
	return filepath.Join(config.EnvInstancePath(), consts.RootfsName)
}

func (config *Config) EnvInstanceWritableRootfsPath() string {
	return filepath.Join(config.EnvInstancePath(), consts.WritableFsName)
}

func (config *Config) CgroupPath() string {
	return filepath.Join(consts.CgroupfsPath, consts.CgroupParentName, config.SandboxID)
}

func (config *Config) PrometheusTargetPath() string {
	return filepath.Join(constants.PrometheusTargetsPath, config.SandboxID+".json")
}

func (config *Config) EnvInstanceCreateSnapshotPath() string {
	return filepath.Join(config.EnvDirPath(), EnvInstancesSnapshotDirName, config.SandboxID)
}

func (config *Config) Ensure(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "create-sandbox-files",
		trace.WithAttributes(
			attribute.String("env_id", config.EnvID),
			attribute.String("sandbox_id", config.SandboxID),
		),
	)
	defer childSpan.End()
	err := os.MkdirAll(config.EnvInstancePath(), 0o777)
	if err != nil {
		errMsg := fmt.Errorf("error making env instance dir: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return errMsg
	}

	telemetry.ReportEvent(childCtx, "env instance directory created")

	err = os.MkdirAll(config.RunningPath(), 0o777)
	if err != nil {
		errMsg := fmt.Errorf("error making env running dir: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return errMsg
	}

	telemetry.ReportEvent(childCtx, "sandbox running directory created")

	err = os.Mkdir(config.CgroupPath(), 0o755)
	if err != nil {
		errMsg := fmt.Errorf("error making cgroup: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return errMsg
	}

	telemetry.ReportEvent(childCtx, "sandbox cgroup created")

	// NOTE(huang-jl): ext4 does not support reflink
	// so we must use xfs
	if config.Overlay {
		// 1. create reflink of writable rootfs file.
		// 2. create a hard link to base read-only rootfs file.
		err = reflink.Always(config.EnvWritableRootfsPath(), config.EnvInstanceWritableRootfsPath())
		if err != nil {
			errMsg := fmt.Errorf("error creating writable reflinked rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
		telemetry.ReportEvent(childCtx, "reflink of writable image created")

		// build a hard link to base rootfs
		err = os.Link(config.EnvRootfsPath(), config.EnvInstanceRootfsPath())
		if err != nil {
			errMsg := fmt.Errorf("error linking base rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
		telemetry.ReportEvent(childCtx, "hard-link of base image created")
	} else {
		err = reflink.Always(config.EnvRootfsPath(), config.EnvInstanceRootfsPath())
		if err != nil {
			errMsg := fmt.Errorf("error creating writable reflinked rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
		telemetry.ReportEvent(childCtx, "reflink of base rootfs created")
	}

	return nil
}

func (config *Config) Cleanup(
	ctx context.Context,
	tracer trace.Tracer,
) error {
	childCtx, childSpan := tracer.Start(ctx, "cleanup-env-instance",
		trace.WithAttributes(
			attribute.String("instance.env_instance_path", config.EnvInstancePath()),
			attribute.String("instance.running_path", config.RunningPath()),
			attribute.String("instance.env_path", config.EnvDirPath()),
		),
	)
	defer childSpan.End()
	var finalErr error

	err := os.RemoveAll(config.EnvInstancePath())
	if err != nil {
		errMsg := fmt.Errorf("error deleting env instance files: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		// TODO: Check the socket?
		telemetry.ReportEvent(childCtx, "removed all env instance files")
	}

	// Remove socket
	err = os.Remove(config.SocketPath)
	if err != nil {
		errMsg := fmt.Errorf("error deleting socket: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed socket")
	}

	err = os.Remove(config.PrometheusTargetPath())
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
		if err = syscall.Rmdir(config.CgroupPath()); err == nil {
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
