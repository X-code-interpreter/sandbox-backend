package cmd

import (
	"github.com/spf13/cobra"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
)

// sandboxCmd represents the sandbox command
var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Operations on the sandbox (i.e., List, Search, Delete)",
}

func init() {
	rootCmd.AddCommand(sandboxCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	sandboxCmd.PersistentFlags().StringP("ip", "i", "127.0.0.1", "the ip address of the backend orchestrator")
	sandboxCmd.PersistentFlags().IntP("port", "p", consts.DefaultOrchestratorPort, "the ip address of the backend orchestrator")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// sandboxCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
