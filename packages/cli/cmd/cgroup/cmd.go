/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cgroup

import (
	"github.com/spf13/cobra"
)

func NewCgroupCommand() *cobra.Command {
	// cgroupCmd represents the cgroup command
	cgroupCmd := &cobra.Command{
		Use:   "cgroup",
		Short: "Do operations on the cgroup of the sandbox.",
		Long: `Do operations on the cgroup of the sanbox.
  This is typically used to reset the stat/counter in the cgroup, e.g., memory.current.

  Note this command should only be executed on the same machine as the orchestrator.
  `,
	}

	cgroupCmd.AddCommand(
		NewRecreateCommand(),
	)

	return cgroupCmd
}
