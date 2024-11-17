package cmd

import (
	"context"
	"fmt"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/lib"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"

	"github.com/spf13/cobra"
)

// purgeCmd represents the purge command
var purgeCmd = &cobra.Command{
	Use:   "purge pid",
	Short: "purge the resource related with a sandbox",
	Long: `In some cases, the orchestrator has crashed but the sandbox (i.e., VM)
  has not been cleanup correctly. This command is used in this scenario. It will
  purges the process, its network resource and the environment. Pass the pid of
  the sandbox (e.g., the process whose cmdline start with 'unshare')`,
	RunE: purgeSandbox,
}

func init() {
	sandboxCmd.AddCommand(purgeCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// purgeCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	purgeCmd.Flags().BoolP("all", "a", false, "Purges all orphan sandboxes")
}

func purgeSandbox(cmd *cobra.Command, args []string) error {
	ip, err := cmd.Flags().GetString("ip")
	if err != nil {
		return err
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return err
	}
	client, err := lib.NewOrchestratorClient(ip, port)
	if err != nil {
		return err
	}
	purgeAll, err := cmd.Flags().GetBool("all")
	if err != nil {
		return err
	}
	req := &orchestrator.SandboxPurgeRequest{PurgeAll: purgeAll, SandboxIDs: args}
	response, err := client.Purge(context.Background(), req)
	if err != nil {
		return fmt.Errorf("purge failed: %v", response)
	}
	if response.Success {
		fmt.Printf("purge succeed, msg: %v", response.Msg)
	} else {
		fmt.Printf("purge does not succeed, msg: %v", response.Msg)
	}
	return nil
}
