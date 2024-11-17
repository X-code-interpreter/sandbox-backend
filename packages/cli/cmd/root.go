package cmd

import (
	"log/slog"
	"os"

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
	rootCmd.Flags().CountVarP(&verbosity, "verbose", "v",
		"the internal log level to print (e.g., -v will print WARNING, -vv will print INFO, -vvv will print DEBUG)",
	)
}

func setupLogger(verbose int) {
	var level slog.LevelVar
	switch verbose {
	case 1:
		level.Set(slog.LevelWarn)
	case 2:
		level.Set(slog.LevelInfo)
	case 3:
		level.Set(slog.LevelDebug)
	default:
		level.Set(slog.LevelError)
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: &level})
	slog.SetDefault(slog.New(handler))
}
