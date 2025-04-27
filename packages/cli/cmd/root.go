package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/X-code-interpreter/sandbox-backend/packages/cli/cmd/cgroup"
	"github.com/X-code-interpreter/sandbox-backend/packages/cli/cmd/network"
	"github.com/X-code-interpreter/sandbox-backend/packages/cli/cmd/sandbox"
	"github.com/spf13/cobra"
)

var verbosity int

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "sandbox-cli",
	Short: "A cli tool to communicate with the backend sandbox server",
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		setupLogger(verbosity)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.PersistentFlags().CountVarP(&verbosity, "verbose", "v",
		"the internal log level to print (e.g., -v will print WARNING, -vv will print INFO, -vvv will print DEBUG)",
	)
	rootCmd.AddCommand(
		sandbox.NewSandboxCommand(),
		cgroup.NewCgroupCommand(),
		network.NewNetworkCommand(),
	)
}

func setupLogger(verbose int) {
	fmt.Printf("set logger with verbose %d\n", verbose)
	var level slog.LevelVar
	switch {
	case verbose == 1:
		level.Set(slog.LevelWarn)
	case verbose == 2:
		level.Set(slog.LevelInfo)
	case verbose >= 3:
		level.Set(slog.LevelDebug)
	default:
		level.Set(slog.LevelError)
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: &level})
	slog.SetDefault(slog.New(handler))
}
