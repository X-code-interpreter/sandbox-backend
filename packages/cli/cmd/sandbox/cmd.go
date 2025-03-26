package sandbox

import (
	"github.com/spf13/cobra"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
)

func NewSandboxCommand() *cobra.Command {
	// sandboxCmd represents the sandbox command
	sandboxCmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Operations on the sandbox (i.e., List, Search, Delete)",
	}
	sandboxCmd.PersistentFlags().StringP("ip", "i", "127.0.0.1", "the ip address of the backend orchestrator")
	sandboxCmd.PersistentFlags().IntP("port", "p", consts.DefaultOrchestratorPort, "the ip address of the backend orchestrator")

	sandboxCmd.AddCommand(
		NewCreateCommand(),
		NewDeleteCommand(),
		NewListCommand(),
		NewPurgeCommand(),
		NewSnapshotCommand(),
	)

	return sandboxCmd
}
