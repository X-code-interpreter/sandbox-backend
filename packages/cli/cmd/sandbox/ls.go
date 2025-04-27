package sandbox

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/lib"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
	"github.com/spf13/cobra"
)

func NewListCommand() *cobra.Command {
	// lsCmd represents the ls command
	lsCmd := &cobra.Command{
		Use:   "ls [-a | -aa] sandbox IDs ...",
		Short: "list sandbox and its information",
		Long: `list sandbox and its information, for example:
  sandbox-cli sandbox ls -a
  sandbox-cli sandbox ls --orphan
  # set the ip address and port of the orchestrator
  sandbox-cli sandbox ls --ip 127.0.0.1 --port 5000 SandboxID-1
  sandbox-cli sandbox ls -i 192.168.47.247 -p 6666 SandboxID-1 SandboxID-2
`,
		SilenceUsage: true,
		RunE:         lsSandbox,
	}
	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// lsCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// lsCmd.Flags().CountP("all", "a", "list all sandboxes (By default only 20). If you want show all, please specify more than one 'a' (e.g., -aa)")
	lsCmd.Flags().BoolP("all", "a", false, "list all sandboxes (excluding orphan).")
	lsCmd.Flags().Bool("orphan", false, "list orphan sandboxes. if not set this flag, only list sandboxes maintained by orchestrator.")
	return lsCmd
}

func lsAll(ip string, port int) error {
	slog.Info("start list all sandbox")
	client, err := lib.NewOrchestratorSbxClient(ip, port)
	if err != nil {
		return err
	}
	ctx := context.Background()
	req := orchestrator.SandboxListRequest{}
	sandboxes, err := client.List(ctx, &req)
	if err != nil {
		return fmt.Errorf("error during sending grpc request: %w", err)
	}
	lib.PrintSandboxInfo("All sandboxes in orchestrator", sandboxes.Sandboxes...)
	return nil
}

func lsOrphan(ip string, port int) error {
	slog.Info("start list orphan sandbox")
	client, err := lib.NewOrchestratorSbxClient(ip, port)
	if err != nil {
		return err
	}
	ctx := context.Background()
	req := orchestrator.SandboxListRequest{Orphan: true}
	sandboxes, err := client.List(ctx, &req)
	if err != nil {
		return fmt.Errorf("error during sending grpc request: %w", err)
	}
	lib.PrintSandboxInfo("Orphan sandboxes", sandboxes.Sandboxes...)
	return nil
}

func lsSubset(ip string, port int, sandboxIDs ...string) error {
	client, err := lib.NewOrchestratorSbxClient(ip, port)
	if err != nil {
		return err
	}
	sandboxes := make([]*orchestrator.SandboxInfo, 0)
	ctx := context.Background()
	for _, sandboxID := range sandboxIDs {
		req := orchestrator.SandboxSearchRequest{SandboxID: sandboxID}
		sbx, err := client.Search(ctx, &req)
		if err != nil {
			slog.Error("sandbox search encounter error", slog.String("sandbox-id", sandboxID))
			continue
		}
		sandboxes = append(sandboxes, sbx.Sandbox)
	}
	lib.PrintSandboxInfo("Sandboxes", sandboxes...)
	return nil
}

func lsSandbox(cmd *cobra.Command, args []string) error {
	ip, err := cmd.Flags().GetString("ip")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator ip from args: %w", err)
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return fmt.Errorf("cannot get orchestrator port from args: %w", err)
	}
	orphan, err := cmd.Flags().GetBool("orphan")
	if err != nil {
		return err
	}
	all, err := cmd.Flags().GetBool("all")
	if err != nil {
		return err
	}
	if all && orphan {
		return fmt.Errorf("cannot specify both --all and --orphan")
	}

	if all {
		if err := lsAll(ip, port); err != nil {
			return fmt.Errorf("error while list all sandbox: %w", err)
		}
	} else if orphan {
		if err := lsOrphan(ip, port); err != nil {
			return fmt.Errorf("error while list all sandbox: %w", err)
		}
	} else {
		if err := lsSubset(ip, port, args...); err != nil {
			return fmt.Errorf("error while list sandbox: %w", err)
		}
	}
	return nil
}
