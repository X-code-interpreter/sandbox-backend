package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	waitSocketTimeout = 10 * time.Second
)

var InvalidSandboxState = errors.New("invalid sandbox state")

// Default MaxIdleConns is 100.
// Default IdleConnTimeout is 90 seconds.
var httpClient = http.Client{
	Timeout: 10 * time.Second,
}

type Sandbox struct {
	mu      sync.Mutex
	vmm     vmm
	Config  *SandboxConfig
	Net     *network.SandboxNetwork
	StartAt time.Time

	waitOnce  sync.Once
	cleanOnce sync.Once
	waitRes   error
	cleanRes  error

	State orchestrator.SandboxState
}

func NewSandbox(
	ctx context.Context,
	tracer trace.Tracer,
	config *SandboxConfig,
	nm *NetworkManager,
) (*Sandbox, error) {
	childCtx, childSpan := tracer.Start(
		ctx,
		"sandbox-new",
		trace.WithAttributes(attribute.String("sandbox.id", config.SandboxID)),
	)
	defer childSpan.End()

	net, err := nm.GetSandboxNetwork(childCtx, tracer, config.SandboxID)
	if err != nil {
		errMsg := fmt.Errorf("failed to get sandbox network: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}
	defer func() {
		if err != nil {
			ntErr := net.Cleanup(childCtx)
			if ntErr != nil {
				errMsg := fmt.Errorf("error cleanup network env after failed sandbox start: %w", ntErr)
				telemetry.ReportError(childCtx, errMsg)
			} else {
				telemetry.ReportEvent(childCtx, "cleanup network env after failed sandbox start")
			}
		}
	}()

	err = config.EnsureFiles(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("failed to create env for FC: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, errMsg
	}

	defer func() {
		if err != nil {
			cleanupErr := config.CleanupFiles(childCtx, tracer, false)
			if cleanupErr != nil {
				errMsg := fmt.Errorf("error deleting env after failed fc start: %w", cleanupErr)
				telemetry.ReportCriticalError(childCtx, errMsg)
			} else {
				telemetry.ReportEvent(childCtx, "cleanup files since new sandbox failed")
			}
		}
	}()

	vmm, err := newVmm(
		childCtx,
		tracer,
		config,
		net,
	)
	if err != nil {
		errMsg := fmt.Errorf("failed to create vmm: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}

	sbx := &Sandbox{
		vmm:     vmm,
		Config:  config,
		Net:     net,
		StartAt: time.Now(),
		State:   orchestrator.SandboxState_RUNNING,
	}

	telemetry.ReportEvent(childCtx, "ensuring clock sync")
	go func() {
		bgCtx, span := tracer.Start(
			context.Background(),
			"sandbox-bg-task",
			trace.WithAttributes(
				attribute.String("sandbox.id", sbx.SandboxID()),
			),
		)
		defer span.End()

		clockErr := sbx.EnsureClockSync(bgCtx)
		if clockErr != nil {
			telemetry.ReportError(bgCtx, fmt.Errorf("failed to sync clock: %w", clockErr))
		} else {
			telemetry.ReportEvent(bgCtx, "clock synced")
		}
		if err := sbx.setupPrometheusTarget(bgCtx, tracer); err != nil {
			telemetry.ReportError(bgCtx, fmt.Errorf("failed to setup prometheus target: %w", err))
		} else {
			telemetry.ReportEvent(bgCtx, "prometheus target set")
		}
	}()

	return sbx, nil
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
	address := fmt.Sprintf("http://%s:%d/sync", s.Net.HostClonedIP(), consts.DefaultEnvdServerPort)

	request, err := http.NewRequestWithContext(ctx, "POST", address, nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}

	// NOTE(huang-jl): After reading the body of response, the http client
	// will reuse the connection
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
) error {
	s.cleanOnce.Do(func() {
		s.cleanRes = s.cleanupAfterFCStop(ctx, tracer)
	})
	return s.cleanRes
}

func (s *Sandbox) cleanupAfterFCStop(
	ctx context.Context,
	tracer trace.Tracer,
) error {
	var (
		err      error
		finalErr error
	)
	childCtx, childSpan := tracer.Start(ctx, "sandbox-delete")
	defer childSpan.End()
	s.mu.Lock()
	defer s.mu.Unlock()
	keepInstanceDir := false

	if s.State != orchestrator.SandboxState_STOP {
		// even this is weird, we still cleanup this fc vm
		// so do not return here
		err = InvalidSandboxState
		errMsg := fmt.Errorf("error during cleanup: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg,
			attribute.String("state", s.State.String()),
			attribute.String("sandbox.id", s.SandboxID()),
		)
		finalErr = errors.Join(finalErr, err)
		// weird state, so we keep instance dir for debugging purpose
		keepInstanceDir = true
	}
	s.State = orchestrator.SandboxState_CLEANNING

	// NOTE(huang-jl): we do not cleanup network here,
	// we try to reuse the network instance.
	// {
	// 	ctx, span := tracer.Start(childCtx, "cleanup-net")
	// 	err = s.Net.Cleanup(ctx)
	// 	span.End()
	// 	if err != nil {
	// 		errMsg := fmt.Errorf("cannot remove network when destroying task: %w", err)
	// 		telemetry.ReportCriticalError(childCtx, errMsg)
	// 		finalErr = errors.Join(finalErr, err)
	// 	} else {
	// 		telemetry.ReportEvent(childCtx, "removed network")
	// 	}
	// }

	err = s.Config.CleanupFiles(childCtx, tracer, keepInstanceDir)
	if err != nil {
		errMsg := fmt.Errorf("failed to delete sandbox files: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		finalErr = errors.Join(finalErr, err)
	} else {
		telemetry.ReportEvent(childCtx, "deleted sandbox files")
	}
	return finalErr
}

func (s *Sandbox) Stop(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "sandbox-stop")
	defer childSpan.End()
	s.mu.Lock()
	defer s.mu.Unlock()
	// despite the state is weird, we still stop the VM
	if s.State != orchestrator.SandboxState_RUNNING {
		err := InvalidSandboxState
		errMsg := fmt.Errorf("error during stop: %w", err)
		telemetry.ReportError(childCtx, errMsg,
			attribute.String("state", s.State.String()),
			attribute.String("sandbox.id", s.SandboxID()),
		)
	}
	// mark the sandbox as KILLING (but the actual delete is in the
	// wait-sandbox goroutine, see Create())
	s.State = orchestrator.SandboxState_STOP
	return s.vmm.stop(childCtx, tracer)
}

// create snaphot of the running vm
//
// @terminate: true to kill the vm, false to resume the vm after generating snapshot
func (s *Sandbox) CreateSnapshot(ctx context.Context, tracer trace.Tracer, terminate bool) error {
	childCtx, childSpan := tracer.Start(ctx, "sandbox-create-snapshot")
	defer childSpan.End()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State != orchestrator.SandboxState_RUNNING {
		err := InvalidSandboxState
		errMsg := fmt.Errorf("error during create snapshot: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg,
			attribute.String("state", s.State.String()),
			attribute.String("sandbox.id", s.SandboxID()),
		)
		return err
	}
	s.State = orchestrator.SandboxState_SNAPSHOTTING
	snapshotDir := s.Config.EnvInstanceCreateSnapshotPath()
	if err := utils.CreateDirAllIfNotExists(snapshotDir, 0o755); err != nil {
		errMsg := fmt.Errorf("failed to create instance snapshot directory: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	}
	if err := s.vmm.Pause(childCtx); err != nil {
		s.State = orchestrator.SandboxState_INVALID
		return err
	}
	if err := s.vmm.Snapshot(childCtx, snapshotDir); err != nil {
		s.State = orchestrator.SandboxState_INVALID
		return err
	}

	if terminate {
		if err := s.vmm.stop(childCtx, tracer); err != nil {
			// no need to report error again
			s.State = orchestrator.SandboxState_INVALID
			return err
		}
		s.State = orchestrator.SandboxState_STOP
	} else {
		// resume
		if err := s.vmm.Resume(childCtx); err != nil {
			s.State = orchestrator.SandboxState_INVALID
			return err
		}
		s.State = orchestrator.SandboxState_RUNNING
	}
	return nil
}

// Wait for the sandbox process has been exited and also
// wait for the cleanup has finished.
//
// This can be called multiple times.
func (s *Sandbox) WaitAndCleanup(ctx context.Context, tracer trace.Tracer) error {
	waitErr := s.Wait()
	cleanErr := s.CleanupAfterFCStop(ctx, tracer)
	return errors.Join(waitErr, cleanErr)
}

// Wait for the sandbox process has been exited, can be called
// multiple times.
func (s *Sandbox) Wait() error {
	s.waitOnce.Do(func() {
		s.waitRes = s.vmm.wait()
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
	f, err := os.OpenFile(s.Config.PrometheusTargetPath(), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o666)
	if err != nil {
		return fmt.Errorf("open prometheus target file (%s) failed: %w", s.Config.PrometheusTargetPath(), err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(config); err != nil {
		return fmt.Errorf("write prometheus target file (%s) failed: %w", s.Config.PrometheusTargetPath(), err)
	}
	return nil
}

func (s *Sandbox) getPid() uint32 {
	return uint32(s.vmm.cmd.Process.Pid)
}

func (s *Sandbox) GetSandboxInfo() orchestrator.SandboxInfo {
	// This is a read only function. Thus, we do not get lock here.
	// Or else, it might conflict with other function (e.g., cleanup).
	sbxPid := s.getPid()
	sbxNetworkIdx := int64(s.Net.NetworkIdx())
	sbxPrivateIp := s.Net.HostClonedIP()
	sbxDiffSnapshot := s.Config.EnableDiffSnapshot
	return orchestrator.SandboxInfo{
		SandboxID:           s.SandboxID(),
		Pid:                 &sbxPid,
		TemplateID:          &s.Config.TemplateID,
		KernelVersion:       &s.Config.KernelVersion,
		NetworkIdx:          &sbxNetworkIdx,
		PrivateIP:           &sbxPrivateIp,
		EnableDiffSnapshots: &sbxDiffSnapshot,
		StartTime:           timestamppb.New(s.StartAt),
		State:               s.State,
	}
}
