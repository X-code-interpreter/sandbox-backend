package sandbox

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"go.opentelemetry.io/otel"
)

func TestEnd2End(t *testing.T) {
	ctx := context.Background()
	tracer := otel.Tracer("")

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	dns, err := NewDNS()
	nm := NewFcNetworkManager()
	if err != nil {
		t.Fatal(err)
	}
	sandboxConfig := &orchestrator.SandboxConfig{
		TemplateID:    "default-code-interpreter",
		KernelVersion: consts.DefaultKernelVersion,
		SandboxID:     "test-end-2-end",
	}
	sandbox, err := NewSandbox(ctx, tracer, dns, sandboxConfig, nm)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Sandbox has started...")
	defer sandbox.CleanupAfterFCStop(ctx, tracer, dns)
	<-ch
	err = sandbox.Stop(ctx, tracer)
	if err != nil {
		t.Logf("error when stop sandbox: %s", err)
		t.Fail()
	}
	sandbox.Wait()
	if err != nil {
		t.Logf("error when wait sandbox: %s", err)
		t.Fail()
	}
}
