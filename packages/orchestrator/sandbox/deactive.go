package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Deactive will try to demote the memory of a sandbox to lower-level
// (e.g., disk via swap).
//
// TODO(huang-jl): use multigen lru (which requires Host Kernel version >= 6.1)
func (s *Sandbox) Deactive(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "sandbox-deactive")
	defer childSpan.End()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State != orchestrator.SandboxState_RUNNING {
		err := InvalidSandboxState
		errMsg := fmt.Errorf("error during deactive: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg,
			attribute.String("state", s.State.String()),
			attribute.String("sandbox.id", s.SandboxID()),
		)
		return err
	}
	cgroupPath := s.Config.CgroupPath()
	// Since (*os.File).Write method will handle EAGAIN internally
	// so I choose to use syscall directly.
	reclaimTrigger, err := syscall.Open(filepath.Join(cgroupPath, "memory.reclaim"), syscall.O_WRONLY, 0)
	if err != nil {
		errMsg := fmt.Errorf("open memory.reclaim for sandbox %s failed: %w", s.SandboxID(), err)
		telemetry.ReportCriticalError(ctx, errMsg)
		return errMsg
	}
	defer syscall.Close(reclaimTrigger)

	telemetry.ReportEvent(ctx, "memory.reclaim file opened")
	// TODO(huang-jl): how to reclaim suitable amount of memory?

	// NOTE that kernel perfers integer, so do not use float here
	// (e.g., use 1500M instead of 1.5G)
	if _, err := syscall.Write(reclaimTrigger, []byte("1500M")); err != nil {
		if err == syscall.EAGAIN {
			telemetry.ReportEvent(ctx, "reclaim finished without reclaim enough memory")
		} else {
			errMsg := fmt.Errorf("write to memory.reclaim for sandbox %s failed: %w", s.SandboxID(), err)
			telemetry.ReportCriticalError(ctx, errMsg)
			return errMsg
		}
	} else {
		telemetry.ReportEvent(ctx, "reclaim succeed")
	}
	return nil
}

func parseMemoryCurrentFile(f *os.File) (int64, error) {
	buf := make([]byte, 64)
	n, err := f.Read(buf)
	if err != nil {
		return 0, fmt.Errorf("read memory.current failed: %w", err)
	}
	byteString := strings.TrimSpace(string(buf[:n]))
	consumption, err := strconv.ParseInt(byteString, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse memory.current (%+v) failed: %w", byteString, err)
	}
	return consumption, nil
}

// Get the memory consumption from host, internally it query
// memory.current file in the cgroup v2.
func (s *Sandbox) HostMemConsumption() (int64, error) {
	cgroupPath := s.Config.CgroupPath()
	currentFile, err := os.Open(filepath.Join(cgroupPath, "memory.current"))
	if err != nil {
		return 0, fmt.Errorf("open memory.current failed: %w", err)
	}
	defer currentFile.Close()
	return parseMemoryCurrentFile(currentFile)
}
