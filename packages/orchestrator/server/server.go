package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/sandbox"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"

	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"github.com/shirou/gopsutil/v4/process"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// server manages sandboxes as provides grpc implmentations
//
// As one machine contains at most thousand of sandboxes,
// I think a map with a mutex is capable of handing this
// scale of data
type server struct {
	orchestrator.UnsafeSandboxServer
	mu         sync.Mutex
	sandboxes  map[string]*sandbox.Sandbox
	dns        *sandbox.DNS
	netManager *sandbox.FcNetworkManager
	tracer     trace.Tracer
	metric     *serverMetric
}

// the second returned value is a cleanup function
// that needs to be called when shutdown the server.
//
// It just stop all the sandboxes
func NewSandboxGrpcServer(logger *zap.Logger) (*grpc.Server, func(), error) {
	grpcSrv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			grpc_zap.UnaryServerInterceptor(logger),
			recovery.UnaryServerInterceptor(),
		),
	)

	logger.Info("Initializing orchestrator server")

	dns, err := sandbox.NewDNS()
	if err != nil {
		return nil, nil, fmt.Errorf("new dns failed: %w", err)
	}

	metric, err := newServerMetric()
	if err != nil {
		return nil, nil, fmt.Errorf("new server metric failed: %w", err)
	}

	s := server{
		dns:        dns,
		sandboxes:  make(map[string]*sandbox.Sandbox),
		netManager: sandbox.NewFcNetworkManager(),
		tracer:     otel.Tracer(constants.ServiceName),
		metric:     metric,
	}

	orchestrator.RegisterSandboxServer(grpcSrv, &s)
	return grpcSrv, func() { s.shutdown() }, nil
}

// Returned bool indicate whether sandbox already exists before insert
func (s *server) InsertSandbox(sbx *sandbox.Sandbox) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := sbx.SandboxID()
	_, ok := s.sandboxes[id]
	s.sandboxes[sbx.SandboxID()] = sbx
	return ok
}

// Returned bool indicate whether find the sandbox
func (s *server) GetSandbox(sandboxID string) (*sandbox.Sandbox, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sbx, ok := s.sandboxes[sandboxID]
	return sbx, ok
}

// Returned bool indicate whether sandboxID exists
func (s *server) DelSandbox(sandboxID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.sandboxes[sandboxID]
	delete(s.sandboxes, sandboxID)
	return ok
}

func (s *server) shutdown() {
	ctx, span := s.tracer.Start(context.Background(), "server-shutdown")
	defer span.End()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sbx := range s.sandboxes {
		sbx.Stop(ctx, s.tracer)
	}
	for _, sbx := range s.sandboxes {
		if err := sbx.WaitAndCleanup(ctx, s.tracer, s.dns); err != nil {
			// record errors during cleanup
			errMsg := fmt.Errorf("wait and cleanup for sandbox failed: %w", err)
			telemetry.ReportError(ctx, errMsg, attribute.String("sandbox.id", sbx.SandboxID()))
		}
	}
}

var envIDRegex *regexp.Regexp = regexp.MustCompile(fmt.Sprintf(`/([\w-]+)/%s/`, sandbox.EnvInstancesDirName))

func (s *server) purgeOne(ctx context.Context, sandboxID string) error {
	var envID string

	fcNetwork, err := s.netManager.SearchFcNetworkByID(ctx, s.tracer, sandboxID)
	if err != nil {
		errMsg := fmt.Errorf("search fc network failed: %v", err)
		telemetry.ReportError(ctx, errMsg, attribute.String("sandbox.id", sandboxID))
		return errMsg
	}
	// Similar to (*Sandbox).cleanupAfterFCStop()
	// 1. kill process
	processes, err := process.Processes()
	if err != nil {
		return fmt.Errorf("cannot get processes on orchestrator: %v", err)
	}
	for _, process := range processes {
		cmdline, err := process.Cmdline()
		if err != nil {
			// TODO(huang-jl): return error or just continue?
			continue
		}
		if strings.HasPrefix(cmdline, "unshare") &&
			strings.Contains(cmdline, "firecracker") &&
			strings.Contains(cmdline, fmt.Sprintf("ip netns exec %s", fcNetwork.NetNsName())) {
			// we find the sandbox process
			if err := process.Kill(); err != nil {
				errMsg := fmt.Errorf("error when killing sandbox process [pid: %d]: %v", process.Pid, err)
				telemetry.ReportError(ctx, errMsg, attribute.String("sandbox.id", sandboxID))
				return errMsg
			}
			envIDMatch := envIDRegex.FindStringSubmatch(cmdline)
			if envIDMatch == nil {
				errMsg := fmt.Errorf("error when parse env id from sandbox: %v", err)
				telemetry.ReportError(ctx, errMsg, attribute.String("sandbox.id", sandboxID))
				return errMsg
			}
			envID = string(envIDMatch[1])
			break
		}
	}

	// 2. cleanup network
	if err := fcNetwork.Cleanup(ctx, s.tracer, s.dns); err != nil {
		errMsg := fmt.Errorf("cleanup fc network failed: %v", err)
		telemetry.ReportError(ctx, errMsg, attribute.String("sandbox.id", sandboxID))
		return errMsg
	}

	// 3. cleanup env
	// we only need EnvInstancePath, SocketPath, CgroupPath and PrometheusTargetPath
	// so skip kernelVersion, kernelsDir, kernelMountDir and firecrackerBinaryPath args
	env, err := sandbox.NewSandboxFiles(ctx, sandboxID, envID, "", "", "", "")
	if err != nil {
		errMsg := fmt.Errorf("new sandbox failed: %v", err)
		telemetry.ReportError(ctx, errMsg, attribute.String("sandbox.id", sandboxID))
	}
	if err := env.Cleanup(ctx, s.tracer); err != nil {
		errMsg := fmt.Errorf("cleanup sandbox file failed: %v", err)
		telemetry.ReportError(ctx, errMsg, attribute.String("sandbox.id", sandboxID))
		return errMsg
	}
	return nil
}
