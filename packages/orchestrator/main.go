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
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/env"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/logging"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
)

func main() {
	var configFile string

	flag.StringVar(&configFile, "config", "", "config file path")
	flag.Parse()
	config, err := server.ParseConfig(configFile)
	if err != nil {
		panic(err)
	}
	logger, err := logging.New(env.IsLocal())
	if err != nil {
		errMsg := fmt.Errorf("create logger failed: %w", err)
		panic(errMsg)
	}
	if !env.IsLocal() {
		shutdown := telemetry.InitOTLPExporter(constants.ServiceName, "no")
		defer shutdown()
	}

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", config.Host, config.Port))
	if err != nil {
		logger.Sugar().Fatalf("failed to listen %s: %v", config.Host, err)
	}

	// Create an instance of our handler which satisfies the generated interface
	s, cleanupFunc, err := server.NewSandboxGrpcServer(logger, config)
	if err != nil {
		logger.Sugar().Fatalf("create grpc server failed: %v", err)
	}

	logger.Sugar().Infof("Starting server on port %d", config.Port)
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
