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
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}

	go func() {
		waitCtx, waitSpan := s.tracer.Start(
			trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(childCtx)),
			"wait-sandbox",
			trace.WithAttributes(
				attribute.String("sandbox.id", sbx.SandboxID()),
			),
		)
		defer waitSpan.End()
		defer telemetry.ReportEvent(waitCtx, "sandbox waited for stopping")
		defer s.metric.DelSandbox(waitCtx, sbx)
		defer s.DelSandbox(req.Sandbox.SandboxID)

		// TODO(huang-jl) put idx backed to network manager?
		defer sbx.CleanupAfterFCStop(waitCtx, s.tracer, s.dns)

		err := sbx.Wait()
		// TODO(huang-jl) Since we use `kill` to stop the FC process
		// the Wait() must return error, should we still report it?
		if err != nil {
			errMsg := fmt.Errorf("failed to wait for Sandbox: %w", err)
			telemetry.ReportCriticalError(waitCtx, errMsg)
		}

		// Wait before removing all resources (see defers above)
		time.Sleep(1 * time.Second)
	}()

	s.InsertSandbox(sbx)
	s.metric.AddSandbox(childCtx, sbx)

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
	childCtx, childSpan := s.tracer.Start(ctx, "sandbox-delete", trace.WithAttributes(
		attribute.String("sandbox.id", req.SandboxID),
	))
	defer childSpan.End()

	sbx, ok := s.GetSandbox(req.SandboxID)
	if !ok {
		errMsg := fmt.Errorf("sandbox not found")
		telemetry.ReportError(childCtx, errMsg)

		return nil, status.New(codes.NotFound, errMsg.Error()).Err()
	}
	// mark the sandbox as KILLING (but the actual delete is in the
	// wait-sandbox goroutine, see Create())
	sbx.State = sandbox.KILLING

	err := sbx.Stop(childCtx, s.tracer)
	if err != nil {
		errMsg := fmt.Errorf("sandbox stop failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

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
		telemetry.ReportError(childCtx, errMsg)

		return nil, status.New(codes.NotFound, errMsg.Error()).Err()
	}

	// 1. first get host mem consumption
	prevConsumption, err := sbx.HostMemConsumption()
	if err != nil {
		errMsg := fmt.Errorf("get prev host memory consumption for sandbox %s failed: %w", sbx.SandboxID(), err)
		telemetry.ReportError(childCtx, errMsg)
		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}
	telemetry.ReportEvent(childCtx, "get prev host memory consumption",
		attribute.Int64("memory.consumption", prevConsumption),
	)

	// 2. deactive the sandbox
	start := time.Now()
	if err := sbx.Deactive(childCtx); err != nil {
		errMsg := fmt.Errorf("deactive sandbox failed: %w", err)
		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}
	s.metric.RecordDeactiveDuration(childCtx, sbx, time.Since(start))

	// 3. get the host memory again to determine how much memory has been saved
	currConsumption, err := sbx.HostMemConsumption()
	if err != nil {
		errMsg := fmt.Errorf("get curr host memory consumption for sandbox %s failed: %w", sbx.SandboxID(), err)
		telemetry.ReportError(childCtx, errMsg)
		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}
	telemetry.ReportEvent(childCtx, "get current host memory consumption",
		attribute.Int64("memory.consumption", currConsumption),
		attribute.Int64("deactive-mem", prevConsumption-currConsumption),
	)

	s.metric.RecordDeactiveMem(childCtx, sbx, prevConsumption-currConsumption)

	return &empty.Empty{}, nil
}
