package lib

import (
	"fmt"
	"net"
	"strconv"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewOrchestratorSbxClient(ip string, port int) (orchestrator.SandboxClient, error) {
	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("found invalid ip address: %s", ip)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("port out of range: %d", port)
	}
	conn, err := grpc.NewClient(
		net.JoinHostPort(ip, strconv.Itoa(port)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create grpc client failed: %w", err)
	}
	return orchestrator.NewSandboxClient(conn), nil
}

func NewOrchestratorHostManageClient(ip string, port int) (orchestrator.HostManageClient, error) {
	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("found invalid ip address: %s", ip)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("port out of range: %d", port)
	}
	conn, err := grpc.NewClient(
		net.JoinHostPort(ip, strconv.Itoa(port)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create grpc client failed: %w", err)
	}
	return orchestrator.NewHostManageClient(conn), nil
}
