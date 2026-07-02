// config_account_remove.go implements "finops config account remove".
package cmd

import (
	"fmt"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/spf13/cobra"
)

var configAccountRemoveCmd = &cobra.Command{
	Use:   "remove <alias>",
	Short: "Remove a registered account alias",
	Long: `Remove an AWS, GCP, or Snowflake account alias from the finops config file.

Example:
  finops config account remove sandbox`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigAccountRemove,
}

func init() {
	configAccountCmd.AddCommand(configAccountRemoveCmd)
}

func runConfigAccountRemove(cmd *cobra.Command, args []string) error {
	alias := args[0]
	path, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		return err
	}
	cfg, err = cfg.RemoveAccountByAlias(alias)
	if err != nil {
		return err
	}
	if err := configstore.Save(path, cfg); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Removed account alias %q from %s\n", alias, path)
	return err
}
