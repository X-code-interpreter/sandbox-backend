package build

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	text_template "text/template"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/template"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"github.com/docker/docker/client"
	"go.opentelemetry.io/otel/trace"
)

//go:embed provision.sh
var provisionEnvScriptFile string
var EnvInstanceTemplate = text_template.Must(text_template.New("provisioning-script").Parse(provisionEnvScriptFile))

type RootfsBuildMode int

const (
	Normal RootfsBuildMode = iota
	// build only rootfs
	BuildOnly
	// skip build rootfs
	SkipBuild
)

var InvalidRootfsBuildMode = errors.New("invalid rootfs build mode")

type Env struct {
	template.VmTemplate

	HypervisorBinaryPath     string          `json:"hypervisorPath,omitempty"`
	StartCmdEnvFilePath      string          `json:"startCmdEnvFilePath,omitempty"`
	StartCmdWorkingDirectory string          `json:"startCmdWorkingDirectory,omitempty"`
	KernelDebugOutput        bool            `json:"kernelDebugOutput"`
	RootfsBuildMode          RootfsBuildMode `json:"rootfsBuildMode"`
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

	err = utils.CreateDirAllIfNotExists(e.RunningPath(), 0o755)
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

	if e.RootfsBuildMode == Normal {
		err := os.RemoveAll(e.RunningPath())
		if err != nil {
			errMsg := fmt.Errorf("error cleaning up env files: %w", err)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "cleaned up env files")
		}
	}
}

func (e *Env) moveSnapshot() error {
	type snapshotFile struct {
		base    string
		dirPath string
	}
	var (
		snapshotFiles []snapshotFile
		tmpFileDir    = e.RunningPath()
	)

	switch e.VmmType {
	case template.FIRECRACKER:
		snapshotFiles = append(snapshotFiles, snapshotFile{
			base:    consts.FcSnapfileName,
			dirPath: tmpFileDir,
		}, snapshotFile{
			base:    consts.FcMemfileName,
			dirPath: tmpFileDir,
		},
		)
	case template.CLOUDHYPERVISOR:
		for _, base := range consts.ChSnapshotFiles {
			snapshotFiles = append(snapshotFiles, snapshotFile{
				base:    base,
				dirPath: tmpFileDir,
			})
		}
	default:
		return template.InvalidVmmType
	}
	for _, file := range snapshotFiles {
		if err := os.Rename(
			filepath.Join(file.dirPath, file.base),
			filepath.Join(e.EnvDirPath(), file.base),
		); err != nil {
			return err
		}
	}
	return nil
}

func (e *Env) MoveToEnvDir(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "move-to-env-dir")
	defer childSpan.End()

	if err := e.moveSnapshot(); err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}

	telemetry.ReportEvent(childCtx, "move snapshot files")

	if err := os.Rename(e.TmpRootfsPath(), e.EnvRootfsPath()); err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}

	telemetry.ReportEvent(childCtx, "moved rootfs")

	if e.Overlay {
		if err := os.Rename(e.TmpWritableRootfsPath(), e.EnvWritableRootfsPath()); err != nil {
			telemetry.ReportCriticalError(childCtx, err)
			return err
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

	switch e.RootfsBuildMode {
	case Normal, BuildOnly:
		_, err = NewRootfs(childCtx, tracer, docker, e)
		if err != nil {
			errMsg := fmt.Errorf("error creating rootfs for env '%s' during build: %w", e.EnvID, err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
	case SkipBuild:
	default:
		return InvalidRootfsBuildMode
	}

	if e.RootfsBuildMode == BuildOnly {
		return nil
	}

	network, err := NewNetworkEnvForSnapshot(childCtx, tracer, e)
	if err != nil {
		errMsg := fmt.Errorf("error network setup for FC while building env '%s' during build: %w", e.EnvID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	defer func() {
		ntErr := network.Cleanup(childCtx)
		if ntErr != nil {
			errMsg := fmt.Errorf("error removing network namespace: %w", ntErr)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "removed network namespace")
		}
	}()

	_, err = NewSnapshot(childCtx, tracer, e, network)
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

// api-socket of vm
func (e *Env) GetSocketPath() string {
	socketFileName := fmt.Sprintf("vmm-build-sock-%s.sock", e.EnvID)
	return filepath.Join(os.TempDir(), socketFileName)
}
