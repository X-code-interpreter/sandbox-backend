package sandbox

import (
	"context"
	"testing"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"go.opentelemetry.io/otel"
)

func TestEnd2End(t *testing.T) {
	ctx := context.Background()
	tracer := otel.Tracer("")
	dns, err := NewDNS()
	if err != nil {
		t.Fatal(err)
	}
	sandboxConfig := &orchestrator.SandboxConfig{
		TemplateID:    "default-cold-interpreter",
		KernelVersion: consts.DefaultKernelVersion,
		SandboxID:     "test-end-2-end",
	}
	sandbox, err := NewSandbox(ctx, tracer, dns, sandboxConfig)
	if err != nil {
		t.Fatal(err)
	}
  t.Log("Sandbox has started...")
	defer sandbox.CleanupAfterFCStop(ctx, tracer, dns)
	err = sandbox.Wait(ctx)
	if err != nil {
		t.Logf("error when wait sandbox: %s", err)
		t.Fail()
	}
}
