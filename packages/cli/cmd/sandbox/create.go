/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package sandbox

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/lib"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func NewCreateCommand() *cobra.Command {
	// createCmd represents the create command
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new sandbox",
		Long: `Create a new sandbox and return its id. For example:

  sandbox-cli sandbox create --template default-sandbox
  # enable diff snapshot
  sandbox-cli sandbox create --template default-sandbox --enable-diff-snapshot
  # set the ip address and port of the orchestrator
  sandbox-cli sandbox create --ip 127.0.0.1 --port 5000 --template mini-agent
`,
		RunE: create,
	}

	createCmd.Flags().StringP("template", "t", "", "The template used for created sandbox")
	createCmd.MarkFlagRequired("template")
	createCmd.Flags().Bool("enable-diff-snapshot", false, "enable diff snapshot for the sandbox (to be used while creating snapshot later)")
	return createCmd
}

func create(cmd *cobra.Command, args []string) error {
	ip, err := cmd.Flags().GetString("ip")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator ip from args: %w", err)
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator port from args: %w", err)
	}
	template, err := cmd.Flags().GetString("template")
	if err != nil {
		return fmt.Errorf("cannot get sandbox template from args: %w", err)
	}

	enableDiffSnapshot, err := cmd.Flags().GetBool("enable-diff-snapshot")
	if err != nil {
		return fmt.Errorf("cannot get enable-diff-snapshot from args: %w", err)
	}
	client, err := lib.NewOrchestratorSbxClient(ip, port)
	if err != nil {
		return err
	}

	sandboxID := uuid.New()
	req := &orchestrator.SandboxCreateRequest{
		TemplateID: template,
		// NOTE(huang-jl): This has not been used for now
		MaxInstanceLength:   3,
		SandboxID:           sandboxID.String(),
		EnableDiffSnapshots: enableDiffSnapshot,
	}
	ctx := context.Background()
	_, err = client.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("sandbox created failed: %w", err)
	}
	slog.Info("sandbox created",
		slog.String("sandbox-id", sandboxID.String()),
		slog.Bool("enable-diff-snapshot", enableDiffSnapshot),
	)
	fmt.Printf("sandbox create succeed, id: %s\n", sandboxID.String())
	return nil
}
