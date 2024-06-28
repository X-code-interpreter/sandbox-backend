package server

import (
	"context"
	"fmt"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/sandbox"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/golang/protobuf/ptypes/empty"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *server) Create(ctx context.Context, req *orchestrator.SandboxCreateRequest) (*orchestrator.SandboxCreateResponse, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "sandbox-create")
	defer childSpan.End()
	childSpan.SetAttributes(
		attribute.String("env.id", req.Sandbox.TemplateID),
		attribute.String("env.kernel.version", req.Sandbox.KernelVersion),
		attribute.String("instance.id", req.Sandbox.SandboxID),
	)

	sandboxConfig := req.Sandbox
	sbx, err := sandbox.NewSandbox(childCtx, s.tracer, s.dns, sandboxConfig, s.netManager)
	if err != nil {
		errMsg := fmt.Errorf("failed to create sandbox: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}

	go func() {
		waitCtx, waitSpan := s.tracer.Start(context.Background(), "wait-sandbox",
			trace.WithAttributes(
				attribute.String("sandbox.id", sbx.SandboxID()),
			),
		)
		defer waitSpan.End()
		defer telemetry.ReportEvent(waitCtx, "sandbox waited for stopping")
		defer s.DelSandbox(req.Sandbox.SandboxID)

		// TODO(huang-jl) put idx backed to network manager?
		defer sbx.CleanupAfterFCStop(waitCtx, s.tracer, s.dns)

		err := sbx.Wait()
		if err != nil {
			errMsg := fmt.Errorf("failed to wait for Sandbox: %w", err)
			telemetry.ReportCriticalError(waitCtx, errMsg)
		}

		// Wait before removing all resources (see defers above)
		time.Sleep(1 * time.Second)
	}()

	s.mu.Lock()
	s.sandboxes[sandboxConfig.SandboxID] = sbx
	s.mu.Unlock()

	return &orchestrator.SandboxCreateResponse{
		PrivateIP: sbx.Network.HostClonedIP(),
	}, nil
}

func (s *server) List(ctx context.Context, _ *empty.Empty) (*orchestrator.SandboxListResponse, error) {
	_, childSpan := s.tracer.Start(ctx, "sandbox-list")
	defer childSpan.End()

	s.mu.Lock()
	items := make([]*sandbox.Sandbox, 0, len(s.sandboxes))
	for _, sbx := range s.sandboxes {
		// only returned running sandbox
		if sbx.State == sandbox.RUNNING {
			items = append(items, sbx)
		}
	}
	s.mu.Unlock()

	sandboxes := make([]*orchestrator.RunningSandbox, 0, len(items))

	for _, sbx := range items {
		sandboxes = append(sandboxes, &orchestrator.RunningSandbox{
			Config:    sbx.Config,
			StartTime: timestamppb.New(sbx.StartAt),
		})
	}

	return &orchestrator.SandboxListResponse{
		Sandboxes: sandboxes,
	}, nil
}

// Delete is a gRPC service that kills a sandbox.
func (s *server) Delete(ctx context.Context, req *orchestrator.SandboxRequest) (*empty.Empty, error) {
	_, childSpan := s.tracer.Start(ctx, "sandbox-delete", trace.WithAttributes(
		attribute.String("sandbox.id", req.SandboxID),
	))
	defer childSpan.End()

	sbx, ok := s.GetSandbox(req.SandboxID)
	if !ok {
		errMsg := fmt.Errorf("sandbox not found")
		telemetry.ReportError(ctx, errMsg)

		return nil, status.New(codes.NotFound, errMsg.Error()).Err()
	}
	// mark the sandbox as KILLING (but the actual delete is in the
	// wait-sandbox goroutine, see Create())
	sbx.State = sandbox.KILLING

	err := sbx.Stop(ctx, s.tracer)
	if err != nil {
		errMsg := fmt.Errorf("sandbox stop failed: %w", err)
		telemetry.ReportCriticalError(ctx, errMsg)

		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}
	// TODO(huang-jl): do we need wait until clean?

	return &empty.Empty{}, nil
}

func (s *server) Deactive(ctx context.Context, req *orchestrator.SandboxRequest) (*empty.Empty, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "sandbox-delete", trace.WithAttributes(
		attribute.String("sandbox.id", req.SandboxID),
	))
	defer childSpan.End()
	sbx, ok := s.GetSandbox(req.SandboxID)
	if !ok {
		errMsg := fmt.Errorf("sandbox not found")
		telemetry.ReportError(ctx, errMsg)

		return nil, status.New(codes.NotFound, errMsg.Error()).Err()
	}
	if err := sbx.Deactive(childCtx); err != nil {
		errMsg := fmt.Errorf("deactive sandbox failed: %w", err)
		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}

	return &empty.Empty{}, nil
}
