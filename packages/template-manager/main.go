package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/template-manager/build"
	"github.com/docker/docker/client"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func Fatal(a ...any) {
	fmt.Fprint(os.Stderr, a...)
	os.Exit(1)
}

func Fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}

// In original e2b, the template-manager is a server
// however, in our situation, we do not need to maintain
// a long-running template-manager, so we use it as a one-shot binary
func main() {
	var (
		cfgPath string
		start   = time.Now()
	)
	flag.StringVar(&cfgPath, "config", "", "path to the template configuration files (e.g., /path/to/config.toml)")
	flag.Parse()
	cfg, err := build.ParseTemplateManagerConfig(cfgPath)
	if err != nil {
		Fatal("cannot parse configuration file: ", err)
	}

	// init otel environment
	ctx := context.Background()
	// we disable metric for template-manager
	shutdown, err := telemetry.InitConsoleOTel(ctx, "template-manager", false)
	if err != nil {
		Fatal("init console otel error: ", err)
	}
	defer shutdown(ctx)

	// There are a bunch of trace generated by docker client
	// so I choose to disable it
	// however metric cannot be disable (as the docker client did provide an option to set it)
	// so leave it alone BAD :(
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
		client.WithTraceProvider(noop.NewTracerProvider()),
	)
	if err != nil {
		Fatal("create docker client error: ", err)
	}

	fmt.Printf("env: %+v\n", cfg)
	if err := cfg.BuildTemplate(ctx, otel.Tracer("template-manager"), dockerClient); err != nil {
		Fatal("build env error: ", err)
	}
	fmt.Printf("build succeed: take %s", time.Since(start))
}
