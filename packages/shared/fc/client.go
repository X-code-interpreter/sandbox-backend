package fc

//go:generate go run github.com/go-swagger/go-swagger/cmd/swagger generate client -f ./firecracker.yaml

import (
	"context"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/client"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/client/operations"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// NOTE(huang-jl): we should not use a single global transport as following:
//
// type socketPathKey struct{}
//
// var socketTransport = &http.Transport{
// 	DialContext: func(ctx context.Context, network, path string) (net.Conn, error) {
// 		socketPath, ok := ctx.Value(socketPathKey{}).(string)
// 		if !ok {
// 			return nil, fmt.Errorf("cannot get socket path from context")
// 		}
// 		addr, err := net.ResolveUnixAddr("unix", socketPath)
// 		if err != nil {
// 			return nil, err
// 		}
//
// 		return net.DialUnix("unix", nil, addr)
// 	},
// 	// copy from DefaultTranport
// 	MaxIdleConns:          100,
// 	IdleConnTimeout:       90 * time.Second,
// 	ExpectContinueTimeout: 1 * time.Second,
// 	TLSHandshakeTimeout:   10 * time.Second,
// }
//
// As the connection reuse logic in http.Transport implementations do not account
// for socket path. A proper way to implement this is to manually keep a Transport
// for each socket path (e.g., using a map[string]*http.Transport).

func NewFirecrackerAPI(socketPath string) *client.FirecrackerAPI {
	httpClient := client.NewHTTPClient(strfmt.NewFormats())

	// create new Transport each time
	// TODO(by huang-jl) as the above comment said:
	// we can optimize this to maintain a pool of transport for individual socket path.
	socketTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, path string) (net.Conn, error) {
			addr, err := net.ResolveUnixAddr("unix", socketPath)
			if err != nil {
				return nil, err
			}

			return net.DialUnix("unix", nil, addr)
		},
	}

	transport := httptransport.New(client.DefaultHost, client.DefaultBasePath, client.DefaultSchemes)
	transport.Transport = socketTransport

	httpClient.SetTransport(transport)
	return httpClient
}

// Wait for firecracker socket to be prepared.
// This function will send get version request to the socket until succeed.
func WaitForSocket(
	ctx context.Context,
	tracer trace.Tracer,
	socketPath string,
	timeout time.Duration,
) (*client.FirecrackerAPI, error) {
	childCtx, childSpan := tracer.Start(ctx, "wait-for-fc-socket")
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
	telemetry.ReportEvent(childCtx, "fc socket created", attribute.Int("retry_times", retryTimes))

	fcClient := NewFirecrackerAPI(socketPath)
	param := operations.NewGetFirecrackerVersionParams().WithContext(childCtx)
	// TODO(huang-jl): use time.After when using golang 1.23 and above?
	retryTimes = 0
	interval := 50 * time.Millisecond
	reqTimer := time.NewTimer(interval)
	defer reqTimer.Stop()
	for {
		if res, err := fcClient.Operations.GetFirecrackerVersion(param); err == nil {
			telemetry.ReportEvent(
				childCtx,
				"fc client get version succeed",
				attribute.String("fc_version", *res.Payload.FirecrackerVersion),
				attribute.Int("retry_times", retryTimes),
			)
			return fcClient, nil
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
