package build

import (
	"context"
	"fmt"
	"runtime"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/template-manager/constants"
	"go.opentelemetry.io/otel/trace"
)

func NewNetworkEnvForSnapshot(ctx context.Context, tracer trace.Tracer, env *Env) (*network.NetworkEnvInfo, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-fc-network")
	defer childSpan.End()

	var err error
	// id and sandboxID here is meaningless, we just set some dummy values
	// because we only setup tap dev here
	info := network.NewNetworkEnvInfo(constants.NetnsNamePrefix+env.EnvID, -1, "")

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	netEnv, err := info.InitEnv()
	if err != nil {
		telemetry.ReportCriticalError(childCtx, fmt.Errorf("error when init network env: %w", err))
		return nil, err
	}
	defer func() {
		netEnv.Exit()
		if err != nil {
			// only need to delete netns when meet error
			info.DeleteNetns()
		}
	}()

	if err = netEnv.SetupNsTapDev(); err != nil {
		telemetry.ReportCriticalError(childCtx, fmt.Errorf("error when setup tap dev: %w", err))
		return nil, err
	}

	return &info, nil
}
