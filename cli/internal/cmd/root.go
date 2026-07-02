// Package cmd wires the finops CLI with Cobra: root command, command groups (core vs setup),
// and registration of noun commands.
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "finops",
	Short: "FinOps command-line tools",
	Long:  "FinOps command-line tools for cost, cluster, and related operations.",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.AddGroup(
		&cobra.Group{ID: "core", Title: "Core Commands:"},
		&cobra.Group{ID: "setup", Title: "Setup:"},
	)
}
