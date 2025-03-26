package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
)

func TestCurrentFileParse(t *testing.T) {
	// 1. first create a cgroup
	cgroupPath := filepath.Join(consts.CgroupfsPath, "test-current-file-parse")
	err := os.Mkdir(cgroupPath, 0o755)
	if err != nil {
		t.Fatalf("create cgroup failed: %s", err)
	}
	defer os.RemoveAll(cgroupPath)

	f, err := os.Open(cgroupPath)
	if err != nil {
		t.Fatalf("open cgroup failed: %s", err)
	}
	defer f.Close()

	// 2. spawn a long running process inside the cgroup
	cmd := exec.Command("bash")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    int(f.Fd()),
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn process failed: %s", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("kill process failed: %s", err)
		}
		cmd.Wait()
	}()

	// 3. parse memory.current
	currentFile, err := os.Open(filepath.Join(cgroupPath, "memory.current"))
	if err != nil {
		t.Fatalf("open memory.current failed: %s", err)
	}
	n, err := parseMemoryCurrentFile(currentFile)
	if err != nil {
		t.Fatalf("parse memory.current failed: %s", err)
	}
	t.Logf("parse memory.current get %d", n)
}
