/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/lib"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/spf13/cobra"
)

// deleteCmd represents the delete command
var deleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "Delete sandbox with sandbox ids",
	Long: `Delete the sandbox with specified ids. To delete orphan sandbox,
please to 'purge' command instead.
Example: 
  sandbox-cli sandbox delete 554a78c8-b80b-48ab-ac60-97c1b4912993
  sandbox-cli sandbox del 554a78c8-b80b-48ab-ac60-97c1b4912993
  sandbox-cli sandbox delete 554a78c8-b80b-48ab-ac60-97c1b4912993 8a8a78c8-b80b-48ab-ac60-97c1b4912992
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ip, err := cmd.Flags().GetString("ip")
		if err != nil {
			return fmt.Errorf("cannot get orchestrator ip from args: %w", err)
		}
		port, err := cmd.Flags().GetInt("port")
		if err != nil {
			return fmt.Errorf("cannot get orchestrator port from args: %w", err)
		}
		client, err := lib.NewOrchestratorClient(ip, port)
		if err != nil {
			return err
		}
		ctx := context.Background()
		var finalErr error

		for _, sandboxID := range args {
			slog.Info("try to delete sandbox", slog.String("sandbox-id", sandboxID))
			req := &orchestrator.SandboxRequest{SandboxID: sandboxID}
			_, err := client.Delete(ctx, req)
			slog.Info("deleted sandbox", slog.String("sandbox-id", sandboxID), slog.Any("error", err))
			finalErr = errors.Join(finalErr, err)
		}
		if finalErr == nil {
			fmt.Printf("delete succeed!")
		}
		return finalErr
	},
}

func init() {
	sandboxCmd.AddCommand(deleteCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// deleteCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// deleteCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
