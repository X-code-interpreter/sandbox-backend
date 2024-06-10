package build

import (
	"context"
	"fmt"
	"net"
	"runtime"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"go.opentelemetry.io/otel/trace"
)

const (
	namespaceNamePrefix = "fc-build-env-"
)

var fcTapCIDR = fmt.Sprintf("%s/%s", consts.FcTapAddress, consts.FcTapMask)

type FcNetwork struct {
	namespaceID string
}

func NewFcNetwork(ctx context.Context, tracer trace.Tracer, env *Env) (*FcNetwork, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-fc-network")
	defer childSpan.End()

	network := &FcNetwork{
		namespaceID: namespaceNamePrefix + env.EnvID,
	}

	err := network.setup(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error setting up network: %w", err)

		network.Cleanup(childCtx, tracer)

		return nil, errMsg
	}

	return network, err
}

// 1. create a new net ns
// 2. create a new tap device in the netns
// 3. setup tap device (in step 2) with ip address of fcTapAddress (169.254.0.22)
//
// When we are taking snapshot, there is no need to make the network connected
// to the host
func (n *FcNetwork) setup(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "setup")
	defer childSpan.End()

	// Prevent thread changes so the we can safely manipulate with namespaces
	telemetry.ReportEvent(childCtx, "waiting for OS thread lock")

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	telemetry.ReportEvent(childCtx, "OS thread lock passed")

	// Save the original (host) namespace and restore it upon function exit
	hostNS, err := netns.Get()
	if err != nil {
		errMsg := fmt.Errorf("cannot get current (host) namespace: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "saved original ns")

	defer func() {
		netErr := netns.Set(hostNS)
		if netErr != nil {
			errMsg := fmt.Errorf("error resetting network namespace back to the host namespace: %w", netErr)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "reset network namespace back to the host namespace")
		}

		netErr = hostNS.Close()
		if netErr != nil {
			errMsg := fmt.Errorf("error closing host network namespace: %w", netErr)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "closed host network namespace")
		}
	}()

	// Create namespace
	ns, err := netns.NewNamed(n.namespaceID)
	if err != nil {
		errMsg := fmt.Errorf("cannot create new namespace: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "created ns")

	defer func() {
		nsErr := ns.Close()
		if nsErr != nil {
			errMsg := fmt.Errorf("error closing namespace: %w", nsErr)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "closed namespace")
		}
	}()

	// Create tap device
	tapAttrs := netlink.NewLinkAttrs()
	tapAttrs.Name = consts.FcTapName
	tapAttrs.Namespace = ns
	tap := &netlink.Tuntap{
		Mode:      netlink.TUNTAP_MODE_TAP,
		LinkAttrs: tapAttrs,
	}

	err = netlink.LinkAdd(tap)
	if err != nil {
		errMsg := fmt.Errorf("error creating tap device: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "created tap device")

	// Active tap device
	err = netlink.LinkSetUp(tap)
	if err != nil {
		errMsg := fmt.Errorf("error setting tap device up: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "set tap device up")

	// Add ip address to tap device
	ip, ipNet, err := net.ParseCIDR(fcTapCIDR)
	if err != nil {
		errMsg := fmt.Errorf("error parsing tap CIDR: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "parsed CIDR")

	err = netlink.AddrAdd(tap, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: ipNet.Mask,
		},
	})
	if err != nil {
		errMsg := fmt.Errorf("error setting address of the tap device: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "set tap device address")

	return nil
}

func (n *FcNetwork) Cleanup(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "cleanup")
	defer childSpan.End()

	err := netns.DeleteNamed(n.namespaceID)
	if err != nil {
		errMsg := fmt.Errorf("error deleting namespace: %w", err)
		telemetry.ReportError(childCtx, errMsg)
		return errMsg
	}
	telemetry.ReportEvent(childCtx, "deleted namespace")
	return nil
}
