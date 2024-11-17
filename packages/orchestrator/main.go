package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/server"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/env"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/logging"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
)

func main() {
	var port int

	flag.IntVar(&port, "port", consts.DefaultOrchestratorPort, "port of orchestrator grpc server")
	flag.IntVar(&port, "p", consts.DefaultOrchestratorPort, "port of orchestrator grpc server")
	flag.Parse()

	logger, err := logging.New(env.IsLocal())
	if err != nil {
		errMsg := fmt.Errorf("create logger failed: %w", err)
		panic(errMsg)
	}
	if !env.IsLocal() {
		shutdown := telemetry.InitOTLPExporter(constants.ServiceName, "no")
		defer shutdown()
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		logger.Sugar().Fatalf("failed to listen: %v", err)
	}

	// Create an instance of our handler which satisfies the generated interface
	s, cleanupFunc, err := server.NewSandboxGrpcServer(logger)
	if err != nil {
		logger.Sugar().Fatalf("create grpc server failed: %v", err)
	}

	logger.Sugar().Infof("Starting server on port %d", port)
	go func() {
		if err := s.Serve(lis); err != nil {
			logger.Sugar().Errorf("failed to serve: %v", err)
		}
	}()

	// graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh
	logger.Sugar().Warnf("Recv signal %d, start to shutdown...", sig)
	s.GracefulStop()
	logger.Sugar().Warnf("start cleanup sandbox...")
	cleanupFunc()
}
