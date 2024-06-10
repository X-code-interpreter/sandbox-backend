package client

import (
	"context"
	"net"
	"net/http"
	"os"
	"time"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
)

func NewFirecrackerAPI(socketPath string) *FirecrackerAPI {
	httpClient := NewHTTPClient(strfmt.NewFormats())

	socketTransport := &http.Transport{
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

func WaitForSocket(socketPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer func() {
		cancel()
		ticker.Stop()
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
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
