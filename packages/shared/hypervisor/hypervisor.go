package hypervisor

import (
	"context"
)

// The abstract interface provided by Sandbox implementation
// (e.g., Cloud Hypervisor or Firecracker), which will be used
// by template manager.
type Hypervisor interface {
	Configure(ctx context.Context) error
	Start(ctx context.Context) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Restore(ctx context.Context, dir string) error
	Snapshot(ctx context.Context, dir string) error
	Cleanup(ctx context.Context) error
}
