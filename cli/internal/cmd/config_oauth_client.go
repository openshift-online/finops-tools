package cmd

import (
	"fmt"
	"os"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/spf13/cobra"
)

var (
	configOAuthClientSetClientID     string
	configOAuthClientSetClientSecret string
	configOAuthClientSetSecretsPath  string
)

var configOAuthClientCmd = &cobra.Command{
	Use:   "oauth-client",
	Short: "Manage OAuth client credentials for Snowflake SSO",
}

var configOAuthClientSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Store Snowflake OAuth client credentials",
	Long: `Store OAuth client ID and secret for Red Hat SSO → Snowflake access.

Credentials are saved outside the main finops config file (default:
~/.config/finops/snowflake-oauth.yaml, mode 0600). Do not commit this file.

You can also set FINOPS_SNOWFLAKE_OAUTH_CLIENT_ID and FINOPS_SNOWFLAKE_OAUTH_CLIENT_SECRET.

Example:
  finops config oauth-client set --client-id finops-tools-dataverse --client-secret "$SECRET"`,
	RunE: runConfigOAuthClientSet,
}

func init() {
	configCmd.AddCommand(configOAuthClientCmd)
	configOAuthClientCmd.AddCommand(configOAuthClientSetCmd)
	configOAuthClientSetCmd.Flags().StringVar(&configOAuthClientSetClientID, "client-id", "", "Red Hat SSO OAuth client ID (required)")
	configOAuthClientSetCmd.Flags().StringVar(&configOAuthClientSetClientSecret, "client-secret", "", "Red Hat SSO OAuth client secret")
	configOAuthClientSetCmd.Flags().StringVar(&configOAuthClientSetSecretsPath, "secrets-file", "",
		"Path to snowflake OAuth secrets file (default: alongside finops config)")
	_ = configOAuthClientSetCmd.MarkFlagRequired("client-id")
}

func runConfigOAuthClientSet(cmd *cobra.Command, _ []string) error {
	secret := configOAuthClientSetClientSecret
	if secret == "" {
		secret = os.Getenv("FINOPS_SNOWFLAKE_OAUTH_CLIENT_SECRET")
	}
	if secret == "" {
		return fmt.Errorf("--client-secret or FINOPS_SNOWFLAKE_OAUTH_CLIENT_SECRET is required")
	}

	path, err := configstore.ResolveSnowflakeOAuthSecretsPath(configOAuthClientSetSecretsPath)
	if err != nil {
		return err
	}
	if err := configstore.SaveSnowflakeOAuthSecrets(path, configstore.SnowflakeOAuthSecrets{
		ClientID:     configOAuthClientSetClientID,
		ClientSecret: secret,
	}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Snowflake OAuth credentials saved to %s\n", path)
	return err
}
