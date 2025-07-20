package sandbox

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/vishvananda/netns"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type SandboxNetworkState int

const (
	invalid SandboxNetworkState = iota
	using
	free
)

type SandboxNetworkWrapper struct {
	network.SandboxNetwork
	state SandboxNetworkState
	mu    sync.Mutex
}

func (net *SandboxNetworkWrapper) SetState(state SandboxNetworkState) SandboxNetworkState {
	net.mu.Lock()
	defer net.mu.Unlock()
	oldState := net.state
	net.state = state
	return oldState
}

func (net *SandboxNetworkWrapper) MakeFree(ctx context.Context, m *NetworkManager) error {
	oldState := net.SetState(free)
	switch oldState {
	case using:
		// delete dns entry
		if err := m.DeleteDNSEntry(net.SandboxID); err != nil {
			errMsg := fmt.Errorf("delete dns entry failed when cleanup network manager: %w", err)
			telemetry.ReportCriticalError(ctx, errMsg)
			net.SetState(invalid)
			net.Cleanup(ctx)
			return errMsg
		}
	case free:
	case invalid:
		return fmt.Errorf("invalid sandbox network")
	}
	return nil
}

type NetworkManager struct {
	mu     sync.Mutex
	nextID int
	// free contains the idle net namespace
	free []int
	// save a reference to all initialized network environment
	// make it easier to cleanup
	// NOTE(huang-jl): maybe an array is enough
	all        map[int]*SandboxNetworkWrapper
	dns        *network.DNS
	VethSubnet *net.IPNet // veth subnet, used to create new SandboxNetwork
}

func NewNetworkManager(dns *network.DNS, vethSubnet *net.IPNet) *NetworkManager {
	// TODO(huang-jl): add background task like create ns if there is few
	// SandboxNetwork in the free array.

	// start from 1
	all := make(map[int]*SandboxNetworkWrapper)
	return &NetworkManager{
		all:        all,
		dns:        dns,
		nextID:     1,
		VethSubnet: vethSubnet,
	}
}

func (m *NetworkManager) Cleanup(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, net := range m.all {
		oldState := net.SetState(invalid)

		switch oldState {
		case invalid:
			continue
		case using:
			// delete dns entry
			if err := m.DeleteDNSEntry(net.SandboxID); err != nil {
				errMsg := fmt.Errorf("delete dns entry failed when cleanup network manager: %w", err)
				telemetry.ReportCriticalError(ctx, errMsg)
			}
		case free:
			// dns entry already been deleted
		}
		net.Cleanup(ctx)
	}
}

func (m *NetworkManager) DNS() *network.DNS {
	return m.dns
}

// create a SandboxNetwork instance and setup the network
func newSandboxNetwork(
	ctx context.Context,
	tracer trace.Tracer,
	idx int,
	subnet *net.IPNet,
) (network.SandboxNetwork, error) {
	childCtx, childSpan := tracer.Start(ctx, "create-sandbox-network", trace.WithAttributes(
		attribute.Int("network_idx", idx),
	))
	defer childSpan.End()
	env := network.NewNetworkEnv(idx, subnet)
	net := network.NewSandboxNetwork(env, "")
	// init network
	if err := setupNetEnv(childCtx, tracer, &net); err != nil {
		net.Cleanup(childCtx)
		return net, err
	}

	return net, nil
}

func (m *NetworkManager) insertUsingNetwork(net *SandboxNetworkWrapper) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.all[net.NetworkIdx()]; ok {
		return fmt.Errorf("duplicated network index: %d", net.NetworkIdx())
	}
	m.all[net.NetworkIdx()] = net
	return nil
}

// When enable `Repurposable`, this will recycle it for later reuse.
// when disable `Repurposable`, this will cleanup the network.
func (m *NetworkManager) RecycleSandboxNetwork(ctx context.Context, net *network.SandboxNetwork) error {
	var recycleMethod string
	m.mu.Lock()
	if net.NetworkIdx() >= m.nextID {
		err := fmt.Errorf("found network idx %d, current max %d", net.NetworkIdx(), m.nextID-1)
		telemetry.ReportCriticalError(ctx, err)
		m.mu.Unlock()
		return err
	}
	wrapper := m.all[net.NetworkIdx()]
	m.mu.Unlock()

	if constants.Repurposable {
		// make it into free queue
		if err := wrapper.MakeFree(ctx, m); err != nil {
			return err
		}
		recycleMethod = "recycle"
		m.mu.Lock()
		m.free = append(m.free, wrapper.NetworkIdx())
		m.mu.Unlock()
	} else {
		// cleanup it
		recycleMethod = "cleanup"
		oldState := wrapper.SetState(invalid)
		switch oldState {
		case invalid:
			return fmt.Errorf("recycle invalid sandbox network (id = %d)", net.NetworkIdx())
		case using:
			// delete dns entry
			if err := m.DeleteDNSEntry(net.SandboxID); err != nil {
				errMsg := fmt.Errorf("delete dns entry failed when cleanup network manager: %w", err)
				telemetry.ReportCriticalError(ctx, errMsg)
			}
		case free:
		}
		if err := wrapper.Cleanup(ctx); err != nil {
			return err
		}
		// delete from map
		m.mu.Lock()
		delete(m.all, net.NetworkIdx())
		m.mu.Unlock()
	}

	telemetry.ReportEvent(ctx, "sandbox network recycled",
		attribute.Int("network_idx", net.NetworkIdx()),
		attribute.String("recycle_method", recycleMethod),
	)
	return nil
}

func (m *NetworkManager) GetSandboxNetwork(
	ctx context.Context,
	tracer trace.Tracer,
	sandboxID string,
) (*network.SandboxNetwork, error) {
	childCtx, childSpan := tracer.Start(ctx, "get-sandbox-network", trace.WithAttributes(
		attribute.String("sandbox.id", sandboxID),
	))
	defer childSpan.End()
	var (
		err     error
		wrapper *SandboxNetworkWrapper
	)
	m.mu.Lock()
	if len(m.free) > 0 {
		// reuse if possible
		idx := m.free[0]
		m.free = m.free[1:]
		wrapper = m.all[idx]
		m.mu.Unlock()
		telemetry.ReportEvent(childCtx, "reuse sandbox network", attribute.Int("idx", idx))
	} else {
		// create a new from scratch
		idx := m.nextID
		m.nextID += 1
		m.mu.Unlock()
		// TODO: A more resonsable judgement relies on subnet size
		if idx > constants.MaxNetworkNumber {
			return nil, fmt.Errorf("network instance number exceed the upper bound")
		}
		net, err := newSandboxNetwork(childCtx, tracer, idx, m.VethSubnet)
		if err != nil {
			return nil, err
		}
		telemetry.ReportEvent(childCtx, "create new sandbox network")
		wrapper = &SandboxNetworkWrapper{
			SandboxNetwork: net,
			state:          using,
		}
		if err := m.insertUsingNetwork(wrapper); err != nil {
			return nil, err
		}
	}

	if err = m.CreateDNSEntry(wrapper.HostClonedIP(), sandboxID); err != nil {
		errMsg := fmt.Errorf("create dns entry failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		// we push it back for later reuse
		m.mu.Lock()
		m.free = append(m.free, wrapper.NetworkIdx())
		m.mu.Unlock()
		return nil, errMsg
	}
	telemetry.ReportEvent(childCtx, "create dns entry")

	wrapper.SetState(using)
	wrapper.SandboxID = sandboxID
	return &wrapper.SandboxNetwork, nil
}

func setupNetEnv(
	ctx context.Context,
	tracer trace.Tracer,
	net *network.SandboxNetwork,
) error {
	childCtx, childSpan := tracer.Start(ctx, "setup-net-env", trace.WithAttributes())
	defer childSpan.End()

	err := net.StartConfigure()
	defer func() {
		if err := net.EndConfigure(); err != nil {
			telemetry.ReportCriticalError(childCtx, err)
		}
	}()
	if err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}

	// first we are in sanbox ns
	if err := net.SetupSbxTapDev(); err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}
	telemetry.ReportEvent(childCtx, "setup sbx tap dev")
	if err := net.SetupSbxLoDev(); err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}
	telemetry.ReportEvent(childCtx, "setup sbx lo dev")
	if err := net.SetupVethPair(); err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}
	telemetry.ReportEvent(childCtx, "setup veth pair")
	if err := net.SetSandboxNs(); err != nil {
		errMsg := fmt.Errorf("change to guest ns failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	}
	if err := net.SetupIptablesAndRoute(); err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}
	telemetry.ReportEvent(childCtx, "setup iptables and route")
	return nil
}

// Typically used to search network information for orphan sandbox.
// For more, check orchestrator grpc about orphan sandbox.
func (m *NetworkManager) SearchNetwork(ctx context.Context, tracer trace.Tracer, netNsName string) (*network.NetworkEnv, error) {
	childCtx, childSpan := tracer.Start(ctx, "search-fc-network-by-id", trace.WithAttributes(
		attribute.String("net_ns_name", netNsName),
	))
	defer childSpan.End()
	netEnv, err := network.ParseNetworkEnvFromNetNsName(netNsName)
	if err != nil {
		errMsg := fmt.Errorf("cannot parse network env from netns name: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}
	// try get netns handle, to confirm the netns still exists
	netNsHandle, err := netns.GetFromName(netNsName)
	if err != nil {
		errMsg := fmt.Errorf("get sandbox netns handle failed: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)
		return nil, errMsg
	}
	netNsHandle.Close()
	return netEnv, nil
}

// can be started in any netns as long as we can access /etc/hosts file.
func (m *NetworkManager) CreateDNSEntry(ip string, sandboxID string) error {
	return m.dns.Add(ip, sandboxID)
}

func (m *NetworkManager) DeleteDNSEntry(sandboxID string) error {
	return m.dns.Remove(sandboxID)
}
