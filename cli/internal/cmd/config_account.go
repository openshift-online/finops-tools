// config_account.go registers "finops config account" for cloud account registry setup.
package cmd

import (
	"github.com/spf13/cobra"
)

var configAccountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage registered cloud accounts",
	Long:  "Add, list, or remove cloud account aliases in the finops config file.",
}

func init() {
	configCmd.AddCommand(configAccountCmd)
	bindAWSPersistentFlags(configAccountCmd)
}
