package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/shirou/gopsutil/v4/process"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/sandbox"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/config"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
)

var (
	_ orchestrator.SandboxServer    = (*server)(nil)
	_ orchestrator.HostManageServer = (*server)(nil)
)

var SandboxNotFound = errors.New("sandbox not found")

func newSandboxConfig(req *orchestrator.SandboxCreateRequest, cfg *OrchestratorConfig) (*sandbox.SandboxConfig, error) {
	var t config.VMTemplate
	templateFilePath := filepath.Join(
		cfg.DataRoot,
		consts.TemplateDirName,
		req.TemplateID,
		consts.TemplateFileName,
	)
	if _, err := toml.DecodeFile(templateFilePath, &t); err != nil {
		return nil, fmt.Errorf("cannot decode template file %s: %w", templateFilePath, err)
	}
	// Assemble socket path
	socketPath, sockErr := sandbox.GetSocketPath(req.SandboxID)
	if sockErr != nil {
		return nil, fmt.Errorf("error getting socket path: %w", sockErr)
	}

	var hypervisorPath string
	if req.HypervisorBinaryPath == nil || len(*req.HypervisorBinaryPath) == 0 {
		switch t.VmmType {
		case config.FIRECRACKER:
			hypervisorPath = cfg.FCBinaryPath
		case config.CLOUDHYPERVISOR:
			hypervisorPath = cfg.CHBinaryPath
		}
	} else {
		hypervisorPath = *req.HypervisorBinaryPath
	}

	return &sandbox.SandboxConfig{
		VMTemplate:           t,
		DataRoot:             cfg.DataRoot,
		SandboxID:            req.SandboxID,
		CgroupName:           cfg.CgroupName,
		SocketPath:           socketPath,
		HypervisorBinaryPath: hypervisorPath,
		EnableDiffSnapshot:   req.EnableDiffSnapshots,
		MaxInstanceLength:    int(req.MaxInstanceLength),
		Metadata:             req.Metadata,
	}, nil
}

func (s *server) NewSandboxConfig(
	ctx context.Context,
	req *orchestrator.SandboxCreateRequest,
) (*sandbox.SandboxConfig, error) {
	_, span := s.tracer.Start(ctx, "new-sandbox-config")
	defer span.End()
	sbxCfg, err := newSandboxConfig(req, s.cfg)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("instance.env_instance_path", sbxCfg.InstancePath()),
		attribute.String("instance.private_dir", sbxCfg.PrivateDir(sbxCfg.DataRoot)),
		attribute.String("instance.template_dir", sbxCfg.TemplateDir(sbxCfg.DataRoot)),
		attribute.String("instance.kernel.host_path", sbxCfg.HostKernelPath(sbxCfg.DataRoot)),
		attribute.String("instance.hypervisor.path", sbxCfg.HypervisorBinaryPath),
		attribute.String("instance.cgroup.path", sbxCfg.CgroupPath()),
	)

	return sbxCfg, nil
}

func (s *server) Create(ctx context.Context, req *orchestrator.SandboxCreateRequest) (*orchestrator.SandboxCreateResponse, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "grpc-create", trace.WithAttributes(
		attribute.String("env.id", req.TemplateID),
		attribute.String("sandbox.id", req.SandboxID),
	))
	defer childSpan.End()

	sbxCfg, err := s.NewSandboxConfig(childCtx, req)
	if err != nil {
		return nil, status.New(codes.InvalidArgument, fmt.Sprintf("cannot create sandbox config: %s", err.Error())).Err()
	}

	// TODO(huang-jl): support attach metadata to sandbox
	sbx, err := sandbox.NewSandbox(childCtx, s.tracer, sbxCfg, s.netManager)
	if err != nil {
		errMsg := fmt.Errorf("failed to create sandbox: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}

	go func() {
		waitCtx, waitSpan := s.tracer.Start(
			context.Background(),
			"wait-sandbox",
			trace.WithAttributes(
				attribute.String("sandbox.id", sbx.SandboxID()),
			),
		)
		defer waitSpan.End()
		defer telemetry.ReportEvent(waitCtx, "sandbox waited for stopping")
		defer s.metric.DelSandbox(waitCtx, sbx)
		defer s.DelSandbox(req.SandboxID)

		// TODO(huang-jl) put idx backed to network manager?
		defer sbx.CleanupAfterFCStop(waitCtx, s.tracer)

		err := sbx.Wait()
		if err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				// NOTE(huang-jl) Since we use `kill` to stop the FC process
				// the Wait() must return error, we do not report it as error here
				status := exiterr.Sys().(syscall.WaitStatus)
				if status.Signaled() && status.Signal() == syscall.SIGKILL {
					telemetry.ReportEvent(waitCtx, "sandbox waited due to sigkill")
				} else {
					errMsg := fmt.Errorf("sandbox waited get non-sigkill signal: %w", err)
					telemetry.ReportError(waitCtx, errMsg)
				}
			} else {
				errMsg := fmt.Errorf("failed to wait for Sandbox: %w", err)
				telemetry.ReportCriticalError(waitCtx, errMsg)
			}
		}

		// TODO(huang-jl): do not sleep
		// Wait before removing all resources (see defers above)
		time.Sleep(1 * time.Second)

		// after wait, we assue the vmm process has already been killed and cleaned
		// so we can reuse the sandbox network
		if err := s.netManager.RecycleSandboxNetwork(ctx, sbx.Net); err != nil {
			errMsg := fmt.Errorf("recycle sandbox network failed: %w", err)
			telemetry.ReportError(ctx, errMsg)
		}
	}()

	s.InsertSandbox(sbx)
	s.metric.AddSandbox(childCtx, sbx)

	sbxInfo := sbx.GetSandboxInfo()
	return &orchestrator.SandboxCreateResponse{
		Info: &sbxInfo,
	}, nil
}

func (s *server) List(ctx context.Context, req *orchestrator.SandboxListRequest) (*orchestrator.SandboxListResponse, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "grpc-list")
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

var (
	sandboxIDRegExp = regexp.MustCompile(fmt.Sprintf(`/%s/([0-9a-zA-Z-]+)`, sandbox.InstancesDirName))
	netNsNameRegExp = regexp.MustCompile(`ip netns exec ([0-9a-zA-Z-]+)`)
)

func (s *server) getSandboxInfoFromProc(ctx context.Context, proc *process.Process) *orchestrator.SandboxInfo {
	cmdline, err := proc.Cmdline()
	if err != nil {
		return nil
	}
	match := sandboxIDRegExp.FindStringSubmatch(cmdline)
	if match == nil {
		err := fmt.Errorf("cannot get sandbox id from cmdline")
		telemetry.ReportCriticalError(ctx, err)
		return nil
	}
	sandboxID := match[1]
	match = netNsNameRegExp.FindStringSubmatch(cmdline)
	if match == nil {
		err := fmt.Errorf("cannot get netns name from cmdline")
		telemetry.ReportCriticalError(ctx, err)
		return nil
	}
	netNsName := match[1]
	netEnv, err := s.netManager.SearchNetwork(ctx, s.tracer, netNsName)
	if err != nil {
		// we find the sandbox but cannot get the Network
		return nil
	}
	templateID, err := parseEnvIdFromOrphanProcess(proc)
	if err != nil {
		telemetry.ReportCriticalError(ctx, err)
		return nil
	}
	// for orphan sandbox, we only populate privateIP and sandboxID
	// NOTE(huang-jl): maybe we can return pid to reduce the overhead for
	// latter purge. But purge is a low-frequent event, so it is fine.
	sbxNetworkIdx := int64(netEnv.NetworkIdx())
	sbxPrivateIP := netEnv.HostClonedIP()
	sbxPid := uint32(proc.Pid)
	return &orchestrator.SandboxInfo{
		SandboxID:  sandboxID,
		Pid:        &sbxPid,
		NetworkIdx: &sbxNetworkIdx,
		PrivateIP:  &sbxPrivateIP,
		TemplateID: &templateID,
		State:      orchestrator.SandboxState_ORPHAN,
	}
}

func (s *server) listOrphan(ctx context.Context) (*orchestrator.SandboxListResponse, error) {
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("cannot get processes on orchestrator: %w", err)
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
		if !strings.Contains(cmdline, constants.FcBinaryName) &&
			!strings.Contains(cmdline, constants.ChBinaryName) {
			continue
		}
		if !strings.Contains(cmdline, "ip netns exec") {
			continue
		}
		info := s.getSandboxInfoFromProc(ctx, process)
		if info == nil {
			continue
		}
		results = append(results, info)
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
		sbxInfo := sbx.GetSandboxInfo()
		results = append(results, &sbxInfo)
	}
	s.mu.Unlock()

	return &orchestrator.SandboxListResponse{
		Sandboxes: results,
	}, nil
}

// Delete is a gRPC service that kills a sandbox.
func (s *server) Delete(ctx context.Context, req *orchestrator.SandboxDeleteRequest) (*empty.Empty, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "grpc-delete", trace.WithAttributes(
		attribute.String("sandbox.id", req.SandboxID),
	))
	defer childSpan.End()

	sbx, ok := s.GetSandbox(req.SandboxID)
	if !ok {
		errMsg := fmt.Errorf("sandbox not found")
		telemetry.ReportError(childCtx, errMsg)

		return nil, status.New(codes.NotFound, errMsg.Error()).Err()
	}

	err := sbx.Stop(childCtx, s.tracer)
	if err != nil {
		errMsg := fmt.Errorf("sandbox stop failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}
	// TODO(huang-jl): do we need wait until clean?

	return &empty.Empty{}, nil
}

func (s *server) Deactive(ctx context.Context, req *orchestrator.SandboxDeactivateRequest) (*empty.Empty, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "grpc-deactive", trace.WithAttributes(
		attribute.String("sandbox.id", req.SandboxID),
	))
	defer childSpan.End()
	sbx, ok := s.GetSandbox(req.SandboxID)
	if !ok {
		err := SandboxNotFound
		telemetry.ReportError(childCtx, err)

		return nil, status.New(codes.NotFound, err.Error()).Err()
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
	if err := sbx.Deactive(childCtx, s.tracer); err != nil {
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

func (s *server) Search(ctx context.Context, req *orchestrator.SandboxSearchRequest) (*orchestrator.SandboxSearchResponse, error) {
	_, childSpan := s.tracer.Start(ctx, "grpc-search", trace.WithAttributes(
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
	sbxInfo := sbx.GetSandboxInfo()
	return &orchestrator.SandboxSearchResponse{
		Sandbox: &sbxInfo,
	}, nil
}

func (s *server) Purge(ctx context.Context, req *orchestrator.SandboxPurgeRequest) (*empty.Empty, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "grpc-purge", trace.WithAttributes(
		attribute.Bool("purge-all", req.PurgeAll),
		attribute.StringSlice("sandbox-ids", req.SandboxIDs),
	))
	defer childSpan.End()
	var (
		finalErr  error
		sandboxes []*orchestrator.SandboxInfo
	)
	if req.PurgeAll {
		orphanSandboxes, err := s.listOrphan(childCtx)
		if err != nil {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		sandboxes = orphanSandboxes.Sandboxes
	} else {
		for _, sandboxID := range req.SandboxIDs {
			process, err := getOrphanProcess(sandboxID)
			if err != nil {
				return nil, status.Error(codes.NotFound, err.Error())
			}
			info := s.getSandboxInfoFromProc(ctx, process)
			if info == nil {
				return nil, status.Error(codes.NotFound, "get sandbox info failed")
			}
			sandboxes = append(sandboxes, info)
		}
	}
	// start to purge
	for _, info := range sandboxes {
		if err := s.purgeOne(childCtx, info); err != nil {
			finalErr = errors.Join(finalErr, err)
		}
	}
	if finalErr != nil {
		return nil, status.Error(codes.NotFound, finalErr.Error())
	} else {
		return &empty.Empty{}, nil
	}
}

func (s *server) Snapshot(ctx context.Context, req *orchestrator.SandboxSnapshotRequest) (*orchestrator.SandboxSnapshotResponse, error) {
	childCtx, childSpan := s.tracer.Start(ctx, "grpc-snapshot", trace.WithAttributes(
		attribute.String("sandbox.id", req.SandboxID),
	))
	defer childSpan.End()

	// NOTE(huang-jl): Do not find in Search() is not considering as error
	sbx, ok := s.GetSandbox(req.SandboxID)
	if !ok {
		err := SandboxNotFound
		telemetry.ReportError(childCtx, err)

		return nil, status.New(codes.NotFound, err.Error()).Err()
	}

	if err := sbx.CreateSnapshot(childCtx, s.tracer, req.Delete); err != nil {
		errMsg := fmt.Errorf("create snapshot failed: %w", err)
		telemetry.ReportError(childCtx, errMsg)

		return nil, status.New(codes.Internal, errMsg.Error()).Err()
	}

	return &orchestrator.SandboxSnapshotResponse{
		Path: sbx.Config.EnvInstanceCreateSnapshotPath(),
	}, nil
}

func (s *server) RecreateCgroup(ctx context.Context, _ *empty.Empty) (*empty.Empty, error) {
	cgroupParentPath := filepath.Join(consts.CgroupfsPath, s.cfg.CgroupName)
	// first remove, and then recreate
	if err := os.Remove(cgroupParentPath); err != nil {
		return nil, status.Errorf(codes.Internal, "remove cgroup failed: %s", err.Error())
	}
	if err := createSandboxCgroup(cgroupParentPath); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &empty.Empty{}, nil
}

func (s *server) CleanNetworkEnv(ctx context.Context, req *orchestrator.HostManageCleanNetworkEnvRequest) (*empty.Empty, error) {
	var finalErr error
	for _, networkIdx := range req.GetNetworkIDs() {
		netEnv := network.NewNetworkEnv(int(networkIdx), s.netManager.VethSubnet)
		// sandbox id is useless here
		net := network.NewSandboxNetwork(netEnv, "")
		if err := net.DeleteNetns(); err != nil {
			finalErr = errors.Join(finalErr, err)
		}
		if err := net.DeleteHostVethDev(); err != nil {
			finalErr = errors.Join(finalErr, err)
		}
		if err := net.DeleteHostIptables(); err != nil {
			finalErr = errors.Join(finalErr, err)
		}
		if err := net.DeleteHostRoute(); err != nil {
			finalErr = errors.Join(finalErr, err)
		}
		s.netManager.DNS().RemoveAddress(net.HostClonedIP())
	}
	if finalErr != nil {
		return nil, status.Error(codes.Internal, finalErr.Error())
	}
	return &empty.Empty{}, nil
}
