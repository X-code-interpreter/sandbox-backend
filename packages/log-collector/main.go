package main

import (
	"context"
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
	"go.uber.org/zap"
)

func main() {
	// first setup logger
	logger, err := logging.New(env.IsLocal())
	if err != nil {
		errMsg := fmt.Errorf("cannot setup logger: %w", err)
		panic(errMsg)
	}
	zap.ReplaceGlobals(logger)
	r := http.NewServeMux()
	r.HandleFunc("/", logcollector.EnvdLogHandler)
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

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	<-c
	ctx, cancel := context.WithTimeout(context.Background(), constants.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		zap.L().Error("server shutdown failed", zap.Error(err))
	}
}
