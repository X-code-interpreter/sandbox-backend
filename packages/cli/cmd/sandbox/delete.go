/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/lib"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/spf13/cobra"
)

func NewDeleteCommand() *cobra.Command {
	deleteCmd := &cobra.Command{
		Use:     "delete",
		Aliases: []string{"del"},
		Short:   "Delete sandbox with sandbox ids",
		Long: `Delete the sandbox with specified ids. To delete orphan sandbox,
please to 'purge' command instead.
Example: 
  sandbox-cli sandbox delete 554a78c8-b80b-48ab-ac60-97c1b4912993
  sandbox-cli sandbox delete 554a78c8-b80b-48ab-ac60-97c1b4912993 8a8a78c8-b80b-48ab-ac60-97c1b4912992
  sandbox-cli sandbox delete --all

  # set the ip address and port of the orchestrator
  sandbox-cli sandbox del -i 192.168.47.247 -p 6666 554a78c8-b80b-48ab-ac60-97c1b4912993 
`,
		RunE: deleteSbx,
	}

	deleteCmd.Flags().BoolP("all", "a", false, "Delete all sandboxes")
	return deleteCmd
}

func deleteAllSandboxes(ctx context.Context, client orchestrator.SandboxClient) error {
	req := orchestrator.SandboxListRequest{}
	sandboxes, err := client.List(ctx, &req)
	if err != nil {
		return fmt.Errorf("error during sending grpc request: %w", err)
	}

	sanboxIDs := make([]string, 0, len(sandboxes.Sandboxes))
	for _, sbx := range sandboxes.Sandboxes {
		sanboxIDs = append(sanboxIDs, sbx.SandboxID)
	}
	return deleteSandboxes(ctx, client, sanboxIDs)
}

func deleteSandboxes(ctx context.Context, client orchestrator.SandboxClient, ids []string) error {
	var finalErr error
	for _, sandboxID := range ids {
		slog.Info("try to delete sandbox", slog.String("sandbox-id", sandboxID))
		req := &orchestrator.SandboxDeleteRequest{SandboxID: sandboxID}
		_, err := client.Delete(ctx, req)
		slog.Info("deleted sandbox", slog.String("sandbox-id", sandboxID), slog.Any("error", err))
		finalErr = errors.Join(finalErr, err)
	}
	return finalErr
}

func askForUserConfirmation(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(prompt, " Do you want to proceed? (y/n): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading input:", err)
			continue
		}

		// Trim whitespace and convert to lowercase
		input = strings.TrimSpace(strings.ToLower(input))

		if input == "y" || input == "yes" {
			return true
		} else if input == "n" || input == "no" {
			return false
		} else {
			fmt.Println("Invalid input. Please enter 'y' or 'n'.")
		}
	}
}

func deleteSbx(cmd *cobra.Command, args []string) error {
	ip, err := cmd.Flags().GetString("ip")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator ip from args: %w", err)
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator port from args: %w", err)
	}
	client, err := lib.NewOrchestratorSbxClient(ip, port)
	if err != nil {
		return err
	}
	deleteAll, err := cmd.Flags().GetBool("all")
	if err != nil {
		return err
	}

	ctx := context.Background()
	if deleteAll {
		if askForUserConfirmation("Delete all sandbox.") {
			// user has confirmed that kill all sandboxes
			err = deleteAllSandboxes(ctx, client)
		} else {
			// user has reject to kill all sandboxes
			return nil
		}
	} else {
		// user pass in sandbox ids to delete
		err = deleteSandboxes(ctx, client, args)
	}

	if err == nil {
		fmt.Println("delete succeed!")
	}
	return err
}
