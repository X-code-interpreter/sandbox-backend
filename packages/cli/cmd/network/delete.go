package network

import (
	"context"
	"fmt"
	"strconv"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/lib"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/spf13/cobra"
)

func NewDeleteCommand() *cobra.Command {
	deleteCmd := &cobra.Command{
		Use:     "delete",
		Aliases: []string{"del"},
		Short:   "Delete the network environment on sandbox host.",
		Long: `Delete the network environment of sandbox, note this SHOULD BE EXECUTED on SANDBOX host.
Be aware that using this command might cause conflict with running orchestrator.
Typically, it should be used after orchestrator crashed with weird network environment.

Example:
sandbox-cli network delete 0
sandbox-cli network delete --ip 127.0.0.1 --port 5000 0 1 2
		`,
		RunE:         deleteSandboxNet,
		SilenceUsage: true,
	}
	return deleteCmd
}

func deleteSandboxNet(cmd *cobra.Command, args []string) error {
	var networkIdxs []int64
	for _, arg := range args {
		idx, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid argument %s", arg)
		}
		networkIdxs = append(networkIdxs, idx)
	}
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

	req := &orchestrator.HostManageCleanNetworkEnvRequest{NetworkIDs: networkIdxs}
	if _, err = client.CleanNetworkEnv(context.Background(), req); err != nil {
		return fmt.Errorf("clean network env failed: %w", err)
	}
	fmt.Println("clean network env succeed")
	return nil
}
