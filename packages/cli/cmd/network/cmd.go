package network

import (
	"github.com/spf13/cobra"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
)

func NewNetworkCommand() *cobra.Command {
	// networkCmd represents the network command
	networkCmd := &cobra.Command{
		Use:   "network",
		Short: "Operations on the network environment",
	}

	networkCmd.PersistentFlags().StringP("ip", "i", "127.0.0.1", "the ip address of the backend orchestrator")
	networkCmd.PersistentFlags().IntP("port", "p", consts.DefaultOrchestratorPort, "the ip address of the backend orchestrator")

	networkCmd.AddCommand(
		NewDeleteCommand(),
	)
	return networkCmd
}
