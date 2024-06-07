package network

import (
	"context"
	"fmt"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network/netns"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"go.opentelemetry.io/otel/trace"
)

type FCNetwork struct {
	netNsName string
	ns        netns.NetNS
}

func NewFCNetwork(ctx context.Context, tracer trace.Tracer, netNsName string) (*FCNetwork, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-fc-network")
	defer childSpan.End()

	network := &FCNetwork{
		netNsName: netNsName,
	}

	err := network.setup(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error setting up network: %w", err)

		network.Cleanup(childCtx, tracer)

		return nil, errMsg
	}

	return network, err
}

func (n *FCNetwork) setup(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "setup")
	defer childSpan.End()

	_, err := netns.NewNetNSNamed(n.netNsName)
	if err != nil {
		errMsg := fmt.Errorf("cannot create new namespace: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "created ns")
	return nil
}

func (n *FCNetwork) Cleanup(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "cleanup")
	defer childSpan.End()

	ns := netns.LoadNetNSNamed(n.netNsName)
	err := ns.Remove()
	if err != nil {
		errMsg := fmt.Errorf("error deleting namespace: %w", err)
		telemetry.ReportError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "deleted namespace")
	}
	return err
}

func (n *FCNetwork) NetNsName() string {
	return n.netNsName
}

func (n *FCNetwork) NetNsPath() string {
	return n.ns.GetPath()
}
