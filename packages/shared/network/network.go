package network

import (
	"context"
	"fmt"
	"sync"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// on single host there should not be too much network
const MaxNetworkNumber = 256 * 60

type NetworkManager struct {
	mu     sync.Mutex
	nextID int64
	// free contains the idle net namespace
	free []int64
}

func NewNetworkManager() *NetworkManager {
	// start from 1
	return &NetworkManager{nextID: 1}
}

func (m *NetworkManager) NewNetworkEnvInfo(sandboxID string) (*NetworkEnvInfo, error) {
	var idx int64
	m.mu.Lock()
	if len(m.free) > 0 {
		idx = m.free[0]
		m.free = m.free[1:]
	} else {
		idx = m.nextID
		m.nextID += 1
	}
	m.mu.Unlock()
	if idx > MaxNetworkNumber {
		return nil, fmt.Errorf("network instance number exceed the upper bound")
	}
	netNsName := GetFcNetNsName(sandboxID)
	info := NewNetworkEnvInfo(netNsName, idx, sandboxID)
	return &info, nil
}

// Typically used to search network information for orphan sandbox.
// For more, check orchestrator grpc about orphan sandbox.
func (nm *NetworkManager) SearchNetworkEnvByID(ctx context.Context, tracer trace.Tracer, sandboxID string) (*NetworkEnvInfo, error) {
	childCtx, childSpan := tracer.Start(ctx, "search-fc-network-by-id", trace.WithAttributes(
		attribute.String("sandbox.id", sandboxID),
	))
	defer childSpan.End()
	// netns of sandbox
	netNsName := GetFcNetNsName(sandboxID)
	netNsHandle, err := netns.GetFromName(netNsName)
	if err != nil {
		errMsg := fmt.Errorf("get sandbox netns handle failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}
	defer netNsHandle.Close()
	netNsIdx, err := netlink.GetNetNsIdByFd(int(netNsHandle))
	if err != nil {
		errMsg := fmt.Errorf("get sandbox netns index failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}

	// iterate through all the veth devices
	// and try to match their netns link with the target netns
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		veth, ok := link.(*netlink.Veth)
		if !ok {
			continue
		}
		if veth.NetNsID == netNsIdx {
			// we find the veth device!
			fcNetIdx := getFcNetIdxFromVethName(veth.Name)
			return &NetworkEnvInfo{
				netNsName: netNsName,
				idx:       int64(fcNetIdx),
				sandboxID: sandboxID,
			}, nil
		}
	}
	return nil, fmt.Errorf("do not find matched fc network for sandbox")
}
