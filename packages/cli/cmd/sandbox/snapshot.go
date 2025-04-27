/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/lib"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/spf13/cobra"
)

func NewSnapshotCommand() *cobra.Command {
	// snapshotCmd represents the snapshot command
	snapshotCmd := &cobra.Command{
		Use:   "snapshot",
		Short: "create snapshot of a running VM",
		Long: `Create snapshot of a running VM. For example:

  sandbox-cli sandbox snapshot SandboxID-1
  # delete the sandbox after generating snapshot, 
  # by default the sandbox will resume after generating snapshot.
  sandbox-cli sandbox snapshot --delete SandboxID-1
  # set the ip address and port of the orchestrator
  sandbox-cli sandbox snapshot --ip 127.0.0.1 --port 5000 SandboxID-1
  sandbox-cli sandbox snapshot -i 192.168.47.247 -p 6666 SandboxID-1 SandboxID-2
.`,
		RunE: snapshot,
	}

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// snapshotCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// snapshotCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	snapshotCmd.Flags().Bool("delete", false, "delete the sandbox after generating snapshot, by default the sandbox will resume after generating snapshot.")
	return snapshotCmd
}

func snapshot(cmd *cobra.Command, args []string) error {
	ip, err := cmd.Flags().GetString("ip")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator ip from args: %w", err)
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator port from args: %w", err)
	}
	terminate, err := cmd.Flags().GetBool("delete")
	if err != nil {
		return fmt.Errorf("cannot get delete from args: %w", err)
	}
	client, err := lib.NewOrchestratorSbxClient(ip, port)
	if err != nil {
		return err
	}

	ctx := context.Background()
	var finalErr error
	for _, sandboxID := range args {
		req := orchestrator.SandboxSnapshotRequest{SandboxID: sandboxID, Delete: terminate}
		response, err := client.Snapshot(ctx, &req)
		slog.Info("snapshoted sandbox", slog.String("sandbox-id", sandboxID), slog.Any("error", err), slog.String("path", response.Path))
		finalErr = errors.Join(finalErr, err)
	}
	return finalErr
}
