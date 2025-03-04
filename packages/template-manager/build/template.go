package build

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	text_template "text/template"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/template"
	"github.com/docker/docker/client"
	"go.opentelemetry.io/otel/trace"
)

const (
	tmpSocketDir = "/tmp"
)

//go:embed provision.sh
var provisionEnvScriptFile string
var EnvInstanceTemplate = text_template.Must(text_template.New("provisioning-script").Parse(provisionEnvScriptFile))

type Env struct {
	template.VmTemplate

	StartCmdEnvFilePath      string `json:"startCmdEnvFilePath,omitempty"`
	StartCmdWorkingDirectory string `json:"startCmdWorkingDirectory,omitempty"`
}

func (e *Env) tmpMemfilePath() string {
	return filepath.Join(e.TmpRunningPath(), consts.MemfileName)
}

func (e *Env) tmpSnapfilePath() string {
	return filepath.Join(e.TmpRunningPath(), consts.SnapfileName)
}

// Dump the env (i.e., the configuration) to json file under [VmTemplate.EnvDirPath].
func (e *Env) dumpEnv(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "dump-env-file")
	defer childSpan.End()

	f, err := os.Create(e.TemplateFilePath())
	if err != nil {
		errMsg := fmt.Errorf("error creating template file: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err = enc.Encode(e.VmTemplate); err != nil {
		errMsg := fmt.Errorf("error encode template: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	return nil
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

	err = os.MkdirAll(e.TmpRunningPath(), 0o777)
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

	err := os.RemoveAll(e.TmpRunningPath())
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

	err := os.Rename(e.tmpSnapfilePath(), e.EnvSnapfilePath())
	if err != nil {
		errMsg := fmt.Errorf("error moving snapshot file: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "moved snapshot file")

	err = os.Rename(e.tmpMemfilePath(), e.EnvMemfilePath())
	if err != nil {
		errMsg := fmt.Errorf("error moving memfile: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "moved memfile")

	err = os.Rename(e.TmpRootfsPath(), e.EnvRootfsPath())
	if err != nil {
		errMsg := fmt.Errorf("error moving rootfs: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "moved rootfs")

	if e.Overlay {
		err = os.Rename(e.TmpWritableRootfsPath(), e.EnvWritableRootfsPath())
		if err != nil {
			errMsg := fmt.Errorf("error moving writable rootfs: %w", err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
	}

	telemetry.ReportEvent(childCtx, "moved writable rootfs")
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

	err = e.dumpEnv(childCtx, tracer)
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
