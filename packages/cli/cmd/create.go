/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/lib"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new sandbox",
	Long: `Create a new sandbox and return its id. For example:

sandbox-cli sandbox create --template default-sandbox.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
		client, err := lib.NewOrchestratorClient(ip, port)
		if err != nil {
			return err
		}

		sandboxID := uuid.New()
		sbxConfig := &orchestrator.SandboxConfig{
			TemplateID:    template,
			KernelVersion: consts.DefaultKernelVersion,
			// NOTE(huang-jl): This has not been used for now
			MaxInstanceLength: 3,
			SandboxID:         sandboxID.String(),
		}
		req := &orchestrator.SandboxCreateRequest{Sandbox: sbxConfig}
		ctx := context.Background()
		_, err = client.Create(ctx, req)
		if err != nil {
			return fmt.Errorf("sandbox created failed: %w", err)
		}
		fmt.Printf("sandbox create succeed, id: %s\n", sandboxID.String())
		return nil
	},
}

func init() {
	sandboxCmd.AddCommand(createCmd)

	createCmd.Flags().StringP("template", "t", "", "The template used for created sandbox")
	createCmd.MarkFlagRequired("template")
}
