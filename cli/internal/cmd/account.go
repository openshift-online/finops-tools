// account.go registers the "finops account" noun command for account billing operations.
package cmd

import (
	"github.com/spf13/cobra"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Account billing and cost",
	Long:  "Fetch cost and usage for registered or targeted cloud accounts.",
}

func init() {
	accountCmd.GroupID = "core"
	bindAWSPersistentFlags(accountCmd)
	rootCmd.AddCommand(accountCmd)
}
