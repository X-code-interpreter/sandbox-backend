package server

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/shirou/gopsutil/v4/process"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/sandbox"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
)

var _ orchestrator.SandboxServer = (*server)(nil)

func (s *server) Create(ctx context.Context, req *orchestrator.SandboxCreateRequest) (*orchestrator.SandboxCreateResponse, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "sandbox-create")
	defer childSpan.End()
	childSpan.SetAttributes(
		attribute.String("env.id", req.Sandbox.TemplateID),
		attribute.String("env.kernel.version", req.Sandbox.KernelVersion),
		attribute.String("sandbox.id", req.Sandbox.SandboxID),
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

		// TODO(huang-jl): do not sleep
		// Wait before removing all resources (see defers above)
		time.Sleep(1 * time.Second)
	}()

	s.InsertSandbox(sbx)
	s.metric.AddSandbox(childCtx, sbx)

	return &orchestrator.SandboxCreateResponse{
		PrivateIP: sbx.Network.HostClonedIP(),
	}, nil
}

func (s *server) List(ctx context.Context, req *orchestrator.SandboxListRequest) (*orchestrator.SandboxListResponse, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "sandbox-list")
	defer childSpan.End()

	var (
		err    error
		result *orchestrator.SandboxListResponse
	)

	if req.Orphan {
		result, err = s.listOrphan(childCtx)
	} else {
		result, err = s.list(childCtx, req.Running)
	}
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}
	return result, nil
}

var sandboxIDRegExp = regexp.MustCompile(`ip netns exec ci-([0-9a-zA-Z-]+)`)

func (s *server) listOrphan(ctx context.Context) (*orchestrator.SandboxListResponse, error) {
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("cannot get processes on orchestrator: %v", err)
	}
	results := make([]*orchestrator.SandboxInfo, 0)
	for _, process := range processes {
		cmdline, err := process.Cmdline()
		if err != nil {
			// TODO(huang-jl): return error or just continue?
			continue
		}
		if !strings.HasPrefix(cmdline, "unshare") {
			continue
		}
		if !strings.Contains(cmdline, "firecracker") {
			continue
		}
		match := sandboxIDRegExp.FindStringSubmatch(cmdline)
		if match == nil {
			continue
		}
		sandboxID := match[1]
		fcNetwork, err := s.netManager.SearchFcNetworkByID(ctx, s.tracer, sandboxID)
		if err != nil {
			// we find the sandbox but cannot get the fcNetwork
			return nil, err
		}
		// for orphan sandbox, we only populate privateIP and sandboxID
		// NOTE(huang-jl): maybe we can return pid to reduce the overhead for
		// latter purge. But purge is a low-frequent event, so it is fine.
		sbxFcNetworkIdx := fcNetwork.FcNetworkIdx()
		sbxPrivateIP := fcNetwork.HostClonedIP()
		sbxPid := uint32(process.Pid)
		results = append(results, &orchestrator.SandboxInfo{
			SandboxID:    sandboxID,
			Pid:          &sbxPid,
			FcNetworkIdx: &sbxFcNetworkIdx,
			PrivateIP:    &sbxPrivateIP,
		})
	}
	return &orchestrator.SandboxListResponse{
		Sandboxes: results,
	}, nil
}

// This function will only list the sandboxes maintained by current orchestrator.
// To list orphan (e.g., sandboxes created by previous crashed orchestrator, see `listOrphan`)
//
// @running: only list sandboxes whose state = running
func (s *server) list(_ context.Context, running bool) (*orchestrator.SandboxListResponse, error) {
	s.mu.Lock()
	results := make([]*orchestrator.SandboxInfo, 0, len(s.sandboxes))
	for _, sbx := range s.sandboxes {
		if running && sbx.State != orchestrator.SandboxState_RUNNING {
			continue
		}
		sbxPid := sbx.GetPid()
		sbxFcNetworkIdx := sbx.Network.FcNetworkIdx()
		sbxPrivateIp := sbx.Network.HostClonedIP()
		results = append(results, &orchestrator.SandboxInfo{
			SandboxID:     sbx.SandboxID(),
			Pid:           &sbxPid,
			TemplateID:    &sbx.Config.TemplateID,
			KernelVersion: &sbx.Config.KernelVersion,
			FcNetworkIdx:  &sbxFcNetworkIdx,
			PrivateIP:     &sbxPrivateIp,
			StartTime:     timestamppb.New(sbx.StartAt),
			State:         sbx.State,
		})
	}
	s.mu.Unlock()

	return &orchestrator.SandboxListResponse{
		Sandboxes: results,
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
	sbx.State = orchestrator.SandboxState_KILLING

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

func (s *server) Search(ctx context.Context, req *orchestrator.SandboxRequest) (*orchestrator.SandboxSearchResponse, error) {
	_, childSpan := s.tracer.Start(ctx, "sandbox-search", trace.WithAttributes(
		attribute.String("sandbox.id", req.SandboxID),
	))
	defer childSpan.End()

	// NOTE(huang-jl): Do not find in Search() is not considering as error
	sbx, ok := s.GetSandbox(req.SandboxID)
	if !ok {
		return &orchestrator.SandboxSearchResponse{
			Sandbox: nil,
		}, nil
	}
	sbxPid := sbx.GetPid()
	sbxFcNetworkIdx := sbx.Network.FcNetworkIdx()
	sbxPrivateIp := sbx.Network.HostClonedIP()
	return &orchestrator.SandboxSearchResponse{
		Sandbox: &orchestrator.SandboxInfo{
			SandboxID:     sbx.SandboxID(),
			Pid:           &sbxPid,
			TemplateID:    &sbx.Config.TemplateID,
			KernelVersion: &sbx.Config.KernelVersion,
			FcNetworkIdx:  &sbxFcNetworkIdx,
			PrivateIP:     &sbxPrivateIp,
			State:         sbx.State,
			StartTime:     timestamppb.New(sbx.StartAt),
		},
	}, nil
}

func (s *server) Purge(ctx context.Context, req *orchestrator.SandboxPurgeRequest) (*orchestrator.SandboxPurgeResponse, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "sandbox-purge", trace.WithAttributes(
		attribute.Bool("purge-all", req.PurgeAll),
		attribute.StringSlice("sandbox-ids", req.SandboxIDs),
	))
	defer childSpan.End()
	var (
		finalErr     error
		sandboxesIDs = req.SandboxIDs
	)
	if req.PurgeAll {
		orphanSandboxes, err := s.listOrphan(childCtx)
		if err != nil {
			return &orchestrator.SandboxPurgeResponse{
				Success: false,
				Msg:     err.Error(),
			}, nil
		} else {
			for _, sbx := range orphanSandboxes.Sandboxes {
				sandboxesIDs = append(sandboxesIDs, sbx.SandboxID)
			}
		}
	}
	// start to purge
	for _, sandboxID := range sandboxesIDs {
		if err := s.purgeOne(childCtx, sandboxID); err != nil {
			finalErr = errors.Join(finalErr, err)
		}
	}
	if finalErr != nil {
		return &orchestrator.SandboxPurgeResponse{
			Success: false,
			Msg:     finalErr.Error(),
		}, nil
	} else {
		return &orchestrator.SandboxPurgeResponse{
			Success: true,
			Msg:     "",
		}, nil
	}
}
