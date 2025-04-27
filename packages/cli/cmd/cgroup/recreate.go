package cgroup

import (
	"context"
	"fmt"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/lib"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spf13/cobra"
)

func NewRecreateCommand() *cobra.Command {
	// cgroupCmd represents the cgroup command
	recreateCmd := &cobra.Command{
		Use:          "recreate",
		Short:        "Recreate the cgroup of the sandbox",
		SilenceUsage: true,
		RunE:         recreate,
	}

	return recreateCmd
}

func recreate(cmd *cobra.Command, args []string) error {
	ip, err := cmd.Flags().GetString("ip")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator ip from args: %w", err)
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator port from args: %w", err)
	}
	client, err := lib.NewOrchestratorHostManageClient(ip, port)
	if err != nil {
		return err
	}
	req := &empty.Empty{}
	if _, err = client.RecreateCgroup(context.Background(), req); err != nil {
		return fmt.Errorf("recreate cgroup failed: %w", err)
	}
	fmt.Println("recreate succeed")
	return nil
}
