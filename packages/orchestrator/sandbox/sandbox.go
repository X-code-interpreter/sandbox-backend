package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	waitSocketTimeout = 2 * time.Second
)

var httpClient = http.Client{
	Timeout: 5 * time.Second,
}

type SandboxState int

const (
	INVALID SandboxState = iota
	RUNNING
	KILLING
)

type Sandbox struct {
	fc      *FcVM
	env     *SandboxFiles
	Config  *orchestrator.SandboxConfig
	Network *FcNetwork
	StartAt time.Time

	waitOnce  sync.Once
	cleanOnce sync.Once
	waitRes   error
	cleanRes  error

	State SandboxState
}

func NewSandbox(
	ctx context.Context,
	tracer trace.Tracer,
	dns *DNS,
	config *orchestrator.SandboxConfig,
	nm *FcNetworkManager,
) (*Sandbox, error) {
	childCtx, childSpan := tracer.Start(
		ctx,
		"new-sandbox",
		trace.WithAttributes(attribute.String("instance.id", config.SandboxID)),
	)
	defer childSpan.End()

	fcNet, err := nm.NewFcNetwork(config.SandboxID)
	if err != nil {
		errMsg := fmt.Errorf("failed to create fc network: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	defer func() {
		if err != nil {
			ntErr := fcNet.Cleanup(childCtx, tracer, dns)
			if ntErr != nil {
				errMsg := fmt.Errorf("error removing network namespace after failed instance start: %w", ntErr)
				telemetry.ReportError(childCtx, errMsg)
			} else {
				telemetry.ReportEvent(childCtx, "removed network namespace")
			}
		}
	}()
	err = fcNet.Setup(childCtx, tracer, dns)
	if err != nil {
		errMsg := fmt.Errorf("failed to setup fc network: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	fsEnv, err := newSandboxFiles(
		childCtx,
		tracer,
		config.SandboxID,
		config.TemplateID,
		config.KernelVersion,
		consts.KernelsDir,
		consts.KernelMountDir,
		constants.FCBinaryPath,
	)
	if err != nil {
		errMsg := fmt.Errorf("failed to assemble env files info for FC: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	telemetry.ReportEvent(childCtx, "assembled env files info")

	err = fsEnv.Ensure(childCtx)
	if err != nil {
		errMsg := fmt.Errorf("failed to create env for FC: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	telemetry.ReportEvent(childCtx, "created env for FC")

	defer func() {
		if err != nil {
			envErr := fsEnv.Cleanup(childCtx, tracer)
			if envErr != nil {
				errMsg := fmt.Errorf("error deleting env after failed fc start: %w", err)
				telemetry.ReportCriticalError(childCtx, errMsg)
			} else {
				telemetry.ReportEvent(childCtx, "deleted env")
			}
		}
	}()

	fc, err := newFCVM(
		childCtx,
		tracer,
		config.SandboxID,
		fsEnv,
		fcNet,
	)
	if err != nil {
		errMsg := fmt.Errorf("failed to new fc vm: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}

	err = fc.startVM(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("failed to start FC: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	instance := &Sandbox{
		fc:      fc,
		env:     fsEnv,
		Config:  config,
		Network: fcNet,
		StartAt: time.Now(),
		State:   RUNNING,
	}

	telemetry.ReportEvent(childCtx, "ensuring clock sync")
	go func() {
		bgCtx, span := tracer.Start(context.Background(), "new-sandbox-bg-task",
			trace.WithAttributes(
				attribute.String("sandbox.id", instance.SandboxID()),
			),
		)
		defer span.End()

		clockErr := instance.EnsureClockSync(bgCtx)
		if clockErr != nil {
			telemetry.ReportError(bgCtx, fmt.Errorf("failed to sync clock: %w", clockErr))
		} else {
			telemetry.ReportEvent(bgCtx, "clock synced")
		}
		if err := instance.setupPrometheusTarget(bgCtx, tracer); err != nil {
			telemetry.ReportError(bgCtx, fmt.Errorf("failed to setup prometheus target: %w", err))
		} else {
			telemetry.ReportEvent(bgCtx, "prometheus target set")
		}
	}()

	return instance, nil
}

func (s *Sandbox) EnsureClockSync(ctx context.Context) error {
syncLoop:
	for {
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		default:
			err := s.syncClock(ctx)
			if err != nil {
				telemetry.ReportError(ctx, fmt.Errorf("error syncing clock: %w", err))
				continue
			}
			break syncLoop
		}
	}

	return nil
}

func (s *Sandbox) syncClock(ctx context.Context) error {
	address := fmt.Sprintf("http://%s:%d/sync", s.Network.HostClonedIP(), consts.DefaultEnvdServerPort)

	request, err := http.NewRequestWithContext(ctx, "POST", address, nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}

	// TODO(huang-jl) why e2b do copying here?
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return err
	}

	defer response.Body.Close()

	return nil
}

// Clean up the resource related to the sandbox (e.g., network, disk...).
// can be called multiple times and will only take effect once.
func (s *Sandbox) CleanupAfterFCStop(
	ctx context.Context,
	tracer trace.Tracer,
	dns *DNS,
) error {
	s.cleanOnce.Do(func() {
		s.cleanRes = s.cleanupAfterFCStop(ctx, tracer, dns)
	})
	return s.cleanRes
}

func (s *Sandbox) cleanupAfterFCStop(
	ctx context.Context,
	tracer trace.Tracer,
	dns *DNS,
) error {
	childCtx, childSpan := tracer.Start(ctx, "delete-instance")
	defer childSpan.End()

	var finalErr error

	err := s.Network.Cleanup(childCtx, tracer, dns)
	if err != nil {
		errMsg := fmt.Errorf("cannot remove network when destroying task: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed network")
	}

	finalErr = errors.Join(finalErr, err)

	err = s.env.Cleanup(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("failed to delete instance files: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "deleted instance files")
	}
	finalErr = errors.Join(finalErr, err)
	return finalErr
}

func (s *Sandbox) Stop(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "stop-sandbox")
	defer childSpan.End()
	return s.fc.stopVM(childCtx, tracer)
}

// Wait for the sandbox process has been exited and also
// wait for the cleanup has finished.
//
// This can be called multiple times.
func (s *Sandbox) WaitAndCleanup(ctx context.Context, tracer trace.Tracer, dns *DNS) error {
	waitErr := s.Wait()
	cleanErr := s.CleanupAfterFCStop(ctx, tracer, dns)
	return errors.Join(waitErr, cleanErr)
}

// Wait for the sandbox process has been exited, can be called
// multiple times.
func (s *Sandbox) Wait() error {
	s.waitOnce.Do(func() {
		s.waitRes = s.fc.wait()
	})
	return s.waitRes
}

func (s *Sandbox) SandboxID() string {
	return s.Config.SandboxID
}

// This will create a json file under sandbox's PrometheusTargetPath.
// The purpose of this file is to inform prometheus the target and path
// of this sandbox.
//
// Since the /metrics endpoint is inside the VM, so the prometheus needs to
// access that endpoint through nginx proxy (which is a container of host network
// mode) which is listened at port 6666.
// And the proxy rules is append the sandbox id and the port inside VM, to the url.
//
// For more about this, you can refer to scripts/nginx.conf and packages/envd.
func (s *Sandbox) setupPrometheusTarget(ctx context.Context, tracer trace.Tracer) error {
	_, childSpan := tracer.Start(ctx, "setup-prometheus-target")
	defer childSpan.End()
	type PrometheusTargetConfig struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels"`
	}
	config := []PrometheusTargetConfig{
		{
			Targets: []string{"host.docker.internal:6666"},
			Labels: map[string]string{
				"id":               s.SandboxID(),
				"__metrics_path__": fmt.Sprintf("/%s/%d/metrics", s.SandboxID(), consts.DefaultEnvdServerPort),
			},
		},
	}
	f, err := os.OpenFile(s.env.PrometheusTargetPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o666)
	if err != nil {
		return fmt.Errorf("open prometheus target file (%s) failed: %w", s.env.PrometheusTargetPath, err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(config); err != nil {
		return fmt.Errorf("write prometheus target file (%s) failed: %w", s.env.PrometheusTargetPath, err)
	}
	return nil
}

// The ctx already contains a span
func (s *Sandbox) Deactive(ctx context.Context) error {
	// TODO(huang-jl): use multigen lru (which requires Host Kernel version >= 6.1)
	cgroupPath := s.env.CgroupPath
	// Since (*os.File).Write method will handle EAGAIN internally
	// so I choose to use syscall directly.
	reclaimTrigger, err := syscall.Open(filepath.Join(cgroupPath, "memory.reclaim"), syscall.O_WRONLY, 0)
	if err != nil {
		errMsg := fmt.Errorf("open memory.reclaim for sandbox %s failed: %w", s.SandboxID(), err)
		telemetry.ReportCriticalError(ctx, errMsg)
		return errMsg
	}
	defer syscall.Close(reclaimTrigger)

	telemetry.ReportEvent(ctx, "memory.reclaim file opened")
	// TODO(huang-jl): how to reclaim suitable amount of memory?
	// NOTE that kernel perfers integer, so do not use float here
	// (e.g., use 1500M instead of 1.5G)
	if _, err := syscall.Write(reclaimTrigger, []byte("1500M")); err != nil {
		if err == syscall.EAGAIN {
			telemetry.ReportEvent(ctx, "reclaim finished without reclaim enough memory")
		} else {
			errMsg := fmt.Errorf("write to memory.reclaim for sandbox %s failed: %w", s.SandboxID(), err)
			telemetry.ReportCriticalError(ctx, errMsg)
			return errMsg
		}
	} else {
		telemetry.ReportEvent(ctx, "reclaim succeed")
	}
	return nil
}
