package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/spf13/cobra"
)

var defaultCgroupPath = filepath.Join(consts.CgroupfsPath, consts.CgroupParentName)

func NewRecreateCommand() *cobra.Command {
	// cgroupCmd represents the cgroup command
	recreateCmd := &cobra.Command{
		Use:   "recreate",
		Short: "Recreate the cgroup of the sandbox",
    SilenceUsage: true,
		RunE:  recreate,
	}
	recreateCmd.Flags().String("name", defaultCgroupPath, "The name of the cgroup to recreate.")

	return recreateCmd
}

func removeCgroup(cgroupPath string) error {
	_, err := os.Stat(cgroupPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s failed: %w", cgroupPath, err)
		}
		// not exist, we just return
		return nil
	}
	// exists and we try to remove it
	if err := os.Remove(cgroupPath); err != nil {
		return fmt.Errorf("remove %s failed: %w", cgroupPath, err)
	}
	return nil
}

func create(cgroupPath string) error {
	if err := os.Mkdir(cgroupPath, 0o755); err != nil {
		return fmt.Errorf("mkdir at %s failed: %w", cgroupPath, err)
	}
	// enable all controllers in controllers into subtree_control
	b, err := os.ReadFile(filepath.Join(cgroupPath, "cgroup.controllers"))
	if err != nil {
		return fmt.Errorf("read cgroup.controllers in %s failed: %w", cgroupPath, err)
	}
	controllers := strings.Fields(string(b))
	for idx, c := range controllers {
		controllers[idx] = "+" + c
	}
	f, err := os.OpenFile(filepath.Join(cgroupPath, "cgroup.subtree_control"), os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open cgroup.subtree_control in %s failed: %w", cgroupPath, err)
	}
	defer f.Close()
	enableRequest := strings.Join(controllers, " ")
	if _, err := f.WriteString(enableRequest); err != nil {
		return fmt.Errorf("write %s to cgroup.subtree_control in %s failed: %w", enableRequest, cgroupPath, err)
	}
	return nil
}

func recreate(cmd *cobra.Command, args []string) error {
	cgroupPath, err := cmd.Flags().GetString("name")
	if err != nil {
		return fmt.Errorf("cannot get name of cgroup from args: %w", err)
	}
	if err := removeCgroup(cgroupPath); err != nil {
		return err
	}
	if err := create(cgroupPath); err != nil {
		return err
	}
	fmt.Printf("recreate %s succeed!\n", cgroupPath)
	return nil
}
