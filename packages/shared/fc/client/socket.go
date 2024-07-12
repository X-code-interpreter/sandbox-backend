package client

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"go.opentelemetry.io/otel/trace"
)

// NOTE(huang-jl): use a global socketTransport for the firecracker client.
// Although firecracker-containerd still uses new http.transport per client,
// I choose to use a global one, as we can control the number of (idle)
// connections and (possibly reuse the connection?).
type socketPathKey struct{}

var socketTransport = &http.Transport{
	DialContext: func(ctx context.Context, network, path string) (net.Conn, error) {
		socketPath, ok := ctx.Value(socketPathKey{}).(string)
		if !ok {
			return nil, fmt.Errorf("cannot get socket path from context")
		}
		addr, err := net.ResolveUnixAddr("unix", socketPath)
		if err != nil {
			return nil, err
		}

		return net.DialUnix("unix", nil, addr)
	},
	// copy from DefaultTranport
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
}

type firecrackerTransport struct {
	transport  runtime.ClientTransport
	socketPath string
}

func newFirecrackerTransport(socketPath string, baseTransport runtime.ClientTransport) *firecrackerTransport {
	return &firecrackerTransport{
		transport:  baseTransport,
		socketPath: socketPath,
	}
}

func (t *firecrackerTransport) Submit(op *runtime.ClientOperation) (interface{}, error) {
	op.Context = context.WithValue(op.Context, socketPathKey{}, t.socketPath)
	return t.transport.Submit(op)
}

func NewFirecrackerAPI(socketPath string) *FirecrackerAPI {
	httpClient := NewHTTPClient(strfmt.NewFormats())

	baseTransport := httptransport.New(DefaultHost, DefaultBasePath, DefaultSchemes)
	baseTransport.Transport = socketTransport

	httpClient.SetTransport(newFirecrackerTransport(socketPath, baseTransport))
	return httpClient
}

func WaitForSocket(
	ctx context.Context,
	tracer trace.Tracer,
	socketPath string,
	timeout time.Duration,
) error {
	childCtx, childSpan := tracer.Start(ctx, "wait-for-fc-socket")
	childCtx, cancel := context.WithTimeout(childCtx, timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer func() {
		cancel()
		ticker.Stop()
		childSpan.End()
	}()
	for {
		select {
		case <-childCtx.Done():
			return childCtx.Err()
		case <-ticker.C:
			if _, err := os.Stat(socketPath); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			// TODO: Send test HTTP request to make sure socket is available
			return nil
		}
	}
}
