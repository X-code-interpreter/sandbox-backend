package ch

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=config.yaml ./cloud-hypervisor.yaml

func NewCloudHypervisorAPI(socketPath string) (*ClientWithResponses, error) {
	socketTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, path string) (net.Conn, error) {
			addr, err := net.ResolveUnixAddr("unix", socketPath)
			if err != nil {
				return nil, err
			}

			return net.DialUnix("unix", nil, addr)
		},
	}
	httpClient := http.Client{
		Transport: socketTransport,
	}

	return NewClientWithResponses("http://localhost/api/v1", WithHTTPClient(&httpClient))
}

func WaitForSocket(ctx context.Context,
	tracer trace.Tracer,
	socketPath string,
	timeout time.Duration,
) (*ClientWithResponses, error) {
	childCtx, childSpan := tracer.Start(ctx, "wait-for-ch-socket")
	childCtx, cancel := context.WithTimeout(childCtx, timeout)

	fileStateTicker := time.NewTicker(10 * time.Millisecond)
	defer func() {
		cancel()
		fileStateTicker.Stop()
		childSpan.End()
	}()

	retryTimes := 0
checkSocketCreation:
	for {
		select {
		case <-childCtx.Done():
			return nil, childCtx.Err()
		case <-fileStateTicker.C:
			if _, err := os.Stat(socketPath); err != nil {
				if os.IsNotExist(err) {
					retryTimes += 1
					continue
				}
				return nil, err
			}
			break checkSocketCreation
		}
	}
	telemetry.ReportEvent(childCtx, "ch socket created", attribute.Int("retry_times", retryTimes))

	chClient, err := NewCloudHypervisorAPI(socketPath)
	if err != nil {
		return nil, err
	}
	// TODO(huang-jl): use time.After when using golang 1.23 and above?
	retryTimes = 0
	interval := 50 * time.Millisecond
	reqTimer := time.NewTimer(interval)
	defer reqTimer.Stop()
	for {
		res, err := chClient.GetVmmPingWithResponse(childCtx)
		if err != nil {
			errMsg := fmt.Errorf("ch client ping error: err %v", err)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			// assert err == nil
			if res.JSON200 != nil {
				telemetry.ReportEvent(
					childCtx,
					"ch client ping vmm succeed",
					attribute.String("ch_version", res.JSON200.Version),
					attribute.Int("retry_times", retryTimes),
				)
				return chClient, nil
			} else {
				errMsg := fmt.Errorf("ch client ping error: err %v, status code = %d", err, res.StatusCode())
				telemetry.ReportError(childCtx, errMsg)
			}
		}
		reqTimer.Reset(interval)
		select {
		case <-childCtx.Done():
			return nil, childCtx.Err()
		case <-reqTimer.C:
			if interval < time.Second {
				interval *= 2
			}
		}
		retryTimes += 1
	}
}
