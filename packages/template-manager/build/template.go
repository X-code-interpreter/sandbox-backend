package build

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/docker/docker/client"
	"go.opentelemetry.io/otel/trace"
)

const (
	tmpSocketDir = "/tmp"
)

//go:embed provision.sh
var provisionEnvScriptFile string
var EnvInstanceTemplate = template.Must(template.New("provisioning-script").Parse(provisionEnvScriptFile))

type Env struct {
	// Unique ID of the env.
	// required
	EnvID string `json:"template"`

	// Command to run when building the env.
	// optional (default: empty)
	StartCmd string `json:"startCmd"`

	// Path to the firecracker binary.
	// optional (default: firecracker)
	FirecrackerBinaryPath string `json:"fcPath"`

	// The number of vCPUs to allocate to the VM.
	// required
	VCpuCount int64 `json:"vcpu"`

	// The amount of RAM memory to allocate to the VM, in MiB.
	// required
	MemoryMB int64 `json:"memMB"`

	// The amount of free disk to allocate to the VM, in MiB.
	// required
	DiskSizeMB int64 `json:"diskMB"`

	// Real size of the rootfs after building the env.
	rootfsSize int64

	// Version of the kernel.
	// optional
	KernelVersion string `json:"kernelVersion"`

	// Docker Image to used as the base image
	// if it is empty, it will be "e2bdev/code-interpreter:latest"
	// optional
	DockerImage string `json:"dockerImg"`

	// Use local docker image (i.e., do not pull from remote docker registry)
	NoPull bool `json:"noPull"`

	HugePages bool `json:"hugePages,omitempty"`
}

// Path to the directory where the env is stored.
func (e *Env) envDirPath() string {
	return filepath.Join(consts.EnvsDisk, e.EnvID)
}

func (e *Env) envRootfsPath() string {
	return filepath.Join(e.envDirPath(), consts.RootfsName)
}

func (e *Env) envMemfilePath() string {
	return filepath.Join(e.envDirPath(), consts.MemfileName)
}

func (e *Env) envSnapfilePath() string {
	return filepath.Join(e.envDirPath(), consts.SnapfileName)
}

func (e *Env) tmpRunningPath() string {
	return filepath.Join(e.envDirPath(), "run")
}

// The running directory where save the rootfs
func (e *Env) tmpRootfsPath() string {
	return filepath.Join(e.tmpRunningPath(), consts.RootfsName)
}

func (e *Env) tmpMemfilePath() string {
	return filepath.Join(e.tmpRunningPath(), consts.MemfileName)
}

func (e *Env) tmpSnapfilePath() string {
	return filepath.Join(e.tmpRunningPath(), consts.SnapfileName)
}

func (e *Env) tmpInstanceID() string {
	return fmt.Sprintf("ci-build-%s", e.EnvID)
}

// The dir on the host where should keep the kernel vmlinux
func (e *Env) KernelDirPath() string {
	return filepath.Join(consts.KernelsDir, e.KernelVersion)
}

// The path of the kernel image path that should passed to FC
func (e *Env) KernelMountPath() string {
	return filepath.Join(consts.KernelMountDir, consts.KernelName)
}

func (e *Env) initialize(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "initialize")
	defer childSpan.End()

	var err error
	defer func() {
		if err != nil {
			e.Cleanup(childCtx, tracer)
		}
	}()

	err = os.MkdirAll(e.tmpRunningPath(), 0o777)
	if err != nil {
		errMsg := fmt.Errorf("error creating tmp build dir: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "created tmp build dir")
	return nil
}

func (e *Env) Cleanup(ctx context.Context, tracer trace.Tracer) {
	childCtx, childSpan := tracer.Start(ctx, "cleanup")
	defer childSpan.End()

	err := os.RemoveAll(e.tmpRunningPath())
	if err != nil {
		errMsg := fmt.Errorf("error cleaning up env files: %w", err)
		telemetry.ReportError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "cleaned up env files")
	}
}

func (e *Env) MoveToEnvDir(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "move-to-env-dir")
	defer childSpan.End()

	err := os.Rename(e.tmpSnapfilePath(), e.envSnapfilePath())
	if err != nil {
		errMsg := fmt.Errorf("error moving snapshot file: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "moved snapshot file")

	err = os.Rename(e.tmpMemfilePath(), e.envMemfilePath())
	if err != nil {
		errMsg := fmt.Errorf("error moving memfile: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "moved memfile")

	err = os.Rename(e.tmpRootfsPath(), e.envRootfsPath())
	if err != nil {
		errMsg := fmt.Errorf("error moving rootfs: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "moved rootfs")
	return nil
}

func (e *Env) Build(ctx context.Context, tracer trace.Tracer, docker *client.Client) error {
	childCtx, childSpan := tracer.Start(ctx, "build")
	defer childSpan.End()

	err := e.initialize(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error initializing directories for building env '%s' during build : %w", e.EnvID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	defer e.Cleanup(childCtx, tracer)

	rootfs, err := NewRootfs(childCtx, tracer, docker, e)
	if err != nil {
		errMsg := fmt.Errorf("error creating rootfs for env '%s' during build: %w", e.EnvID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	network, err := NewFcNetwork(childCtx, tracer, e)
	if err != nil {
		errMsg := fmt.Errorf("error network setup for FC while building env '%s' during build: %w", e.EnvID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	defer func() {
		ntErr := network.Cleanup(childCtx, tracer)
		if ntErr != nil {
			errMsg := fmt.Errorf("error removing network namespace: %w", ntErr)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "removed network namespace")
		}
	}()

	_, err = NewSnapshot(childCtx, tracer, e, network, rootfs)
	if err != nil {
		errMsg := fmt.Errorf("error snapshot for env '%s' during build: %w", e.EnvID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	err = e.MoveToEnvDir(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error moving env files to their final destination during while building env '%s' during build: %w", e.EnvID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	return nil
}

// api-socket of FC
func (e *Env) getSocketPath() string {
	socketFileName := fmt.Sprintf("fc-build-sock-%s.sock", e.EnvID)
	return filepath.Join(tmpSocketDir, socketFileName)
}
