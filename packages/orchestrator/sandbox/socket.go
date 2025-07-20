package sandbox

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
)

func GetSocketPath(sandboxID string) (string, error) {
	filename := "vmm-" + sandboxID + ".socket"
	if dir := os.TempDir(); utils.CheckDirExists(dir) {
		return filepath.Join(dir, filename), nil
	} else {
		return "", fmt.Errorf("unable to find a location for vmm socket")
	}
}
