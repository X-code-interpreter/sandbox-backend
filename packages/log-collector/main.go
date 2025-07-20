package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/X-code-interpreter/sandbox-backend/packages/log-collector/constants"
	logcollector "github.com/X-code-interpreter/sandbox-backend/packages/log-collector/server"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/env"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/logging"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"go.uber.org/zap"
)

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "", "config file path")
	flag.Parse()

	// first setup logger
	logger, err := logging.New(env.IsLocal())
	if err != nil {
		panic(fmt.Errorf("cannot setup logger: %w", err))
	}
	zap.ReplaceGlobals(logger)

	cfg, err := logcollector.ParseLogCollectorConfig(configFile)
	if err != nil {
		panic(fmt.Errorf("cannot parse config file: %w", err))
	}
	if err := utils.CreateDirAllIfNotExists(cfg.LogDir(), 0o755); err != nil {
		panic(fmt.Errorf("cannot create log directory: %w", err))
	}

	c := logcollector.NewLogCollector(cfg)
	r := http.NewServeMux()
	r.HandleFunc("/", c.EnvdLogHandler)
	srv := http.Server{
		Addr:    fmt.Sprintf(":%d", consts.DefaultLogCollectorPort),
		Handler: r,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			zap.L().Error("listen and server failed", zap.Error(err))
		}
	}()
	zap.L().Info("server start...", zap.Int("port", consts.DefaultLogCollectorPort))

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), constants.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		zap.L().Error("server shutdown failed", zap.Error(err))
	}
}
