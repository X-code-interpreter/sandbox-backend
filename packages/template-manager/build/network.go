package build

import (
	"context"
	"fmt"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/template-manager/constants"
	"go.opentelemetry.io/otel/trace"
)

func NewNetworkEnvForSnapshot(
	ctx context.Context,
	tracer trace.Tracer,
	c *TemplateManagerConfig,
) (*network.SandboxNetwork, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-template-network")
	defer childSpan.End()

	var err error
	// id and sandboxID here is meaningless, we just set some dummy values.
	// BTW, the orchestrator will use idx started from 1, so 0 here is safe.
	netEnv := network.NewNetworkEnv(0, c.Subnet.IPNet)
	net := network.NewSandboxNetwork(netEnv, constants.NetnsNamePrefix+c.TemplateID)

	err = net.StartConfigure()
	defer func() {
		if endCfgErr := net.EndConfigure(); endCfgErr != nil {
			errMsg := fmt.Errorf("end network configuration err: %w", endCfgErr)
			telemetry.ReportCriticalError(ctx, errMsg)
		}
		if err != nil {
			// only need to delete netns when meet error
			net.DeleteNetns()
		}
	}()
	if err != nil {
		telemetry.ReportCriticalError(childCtx, fmt.Errorf("error when init network env: %w", err))
		return nil, err
	}
	if err = net.SetupSbxTapDev(); err != nil {
		telemetry.ReportCriticalError(childCtx, fmt.Errorf("error when setup tap dev: %w", err))
		return nil, err
	}

	return &net, nil
}
