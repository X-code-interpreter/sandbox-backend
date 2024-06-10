package sandbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

type Sandbox struct {
	fc      *FcVM
	env     *SandboxFiles
	config  *orchestrator.SandboxConfig
	network *FcNetwork
	startAt time.Time
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
		config:  config,
		network: fcNet,
		startAt: time.Now(),
	}
	telemetry.ReportEvent(childCtx, "ensuring clock sync")

	go func() {
		backgroundCtx := context.Background()

		clockErr := instance.EnsureClockSync(backgroundCtx)
		if clockErr != nil {
			telemetry.ReportError(backgroundCtx, fmt.Errorf("failed to sync clock: %w", clockErr))
		} else {
			telemetry.ReportEvent(backgroundCtx, "clock synced")
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
	address := fmt.Sprintf("http://%s:%d/sync", s.network.HostClonedIP(), consts.DefaultEnvdServerPort)

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

func (s *Sandbox) CleanupAfterFCStop(
	ctx context.Context,
	tracer trace.Tracer,
	dns *DNS,
) {
	childCtx, childSpan := tracer.Start(ctx, "delete-instance")
	defer childSpan.End()

	err := s.network.Cleanup(childCtx, tracer, dns)
	if err != nil {
		errMsg := fmt.Errorf("cannot remove network when destroying task: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "removed network")
	}

	err = s.env.Cleanup(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("failed to delete instance files: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "deleted instance files")
	}

	// TODO(huang-jl) put idx backed to network manager?
}

func (s *Sandbox) Stop(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "stop-sandbox", trace.WithAttributes())
	defer childSpan.End()
	return s.fc.stopVM(childCtx, tracer)
}

func (s *Sandbox) Wait() error {
	return s.fc.wait()
}
