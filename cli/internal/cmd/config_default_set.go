// config_default_set.go implements "finops config default set" to persist a default value.
package cmd

import (
	"fmt"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/spf13/cobra"
)

var (
	configDefaultSetName   string
	configDefaultSetValue  string
	configDefaultSetConfig string
)

var configDefaultSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set a configuration default",
	Long: `Store a default value in the finops config file (fully qualified names).

Supported names:
  aws.auth-method — AWS authentication for account add (saml or profile)
  aws.linked_role — IAM role name for linked account add (default: OrganizationAccountAccessRole)
  cost.days — default lookback in calendar days for account get-cost / report create
  cost.months — default lookback in calendar months (1st-of-month start)
  cost.from — default start date YYYY-MM-DD (optional cost.to)
  cost.to — default end date YYYY-MM-DD (requires cost.from)
  cost.exclude_recent_days — omit last N UTC days from the cost end anchor (AWS CE lag)
  gcp.auth-method — reserved for future GCP support
  snowflake.sso_issuer — Red Hat SSO for Snowflake: prod or stage
  snowflake.oauth_audience — JWT audience for Snowflake OAuth (default: dataverse-snowflake)
  snowflake.account_alias — default Snowflake account alias for finops snowflake commands (set automatically on first account add)
  snowflake.warehouse — default warehouse when an alias omits warehouse (required for queries unless set per alias)
  snowflake.role — default role when an alias omits role
  snowflake.database — default database when an alias omits database
  snowflake.schema — default schema when an alias omits schema

Example:
  finops config default set --name aws.auth-method --value profile
  finops config default set --name aws.linked_role --value OrganizationAccountAccessRole
  finops config default set --name cost.exclude_recent_days --value 2`,
	RunE: runConfigDefaultSet,
}

func init() {
	configDefaultCmd.AddCommand(configDefaultSetCmd)
	configDefaultSetCmd.Flags().StringVar(&configDefaultSetName, "name", "", "Default name (required)")
	configDefaultSetCmd.Flags().StringVar(&configDefaultSetValue, "value", "", "Default value (required)")
	configDefaultSetCmd.Flags().StringVar(&configDefaultSetConfig, "config", "", "Path to finops config file (default: OS-specific config dir)")
	_ = configDefaultSetCmd.MarkFlagRequired("name")
	_ = configDefaultSetCmd.MarkFlagRequired("value")
}

func runConfigDefaultSet(cmd *cobra.Command, _ []string) error {
	path, err := configstore.ResolvePath(configDefaultSetConfig)
	if err != nil {
		return err
	}
	if err := configstore.SetDefault(path, configDefaultSetName, configDefaultSetValue); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Default %s set to %q in %s\n",
		configDefaultSetName, configDefaultSetValue, path)
	return err
}
