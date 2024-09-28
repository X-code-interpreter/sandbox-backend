package client

import (
	"context"
	"net"
	"net/http"
	"os"
	"time"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
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

func NewFirecrackerAPI(socketPath string) *FirecrackerAPI {
	httpClient := NewHTTPClient(strfmt.NewFormats())

  // create new Transport each time
  // TODO(by huang-jl) as the above comment said:
  // we can optimize this to maintain a pool of transport for individual socket path.
  socketTransport := &http.Transport {
    DialContext: func(ctx context.Context, network, path string) (net.Conn, error) {
			addr, err := net.ResolveUnixAddr("unix", socketPath)
			if err != nil {
				return nil, err
			}

			return net.DialUnix("unix", nil, addr)
    },
  }

	transport := httptransport.New(DefaultHost, DefaultBasePath, DefaultSchemes)
	transport.Transport = socketTransport

	httpClient.SetTransport(transport)
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
