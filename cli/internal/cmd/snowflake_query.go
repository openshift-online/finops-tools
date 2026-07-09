package cmd

import (
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/snowflakeoauth"
	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
	"github.com/spf13/cobra"
)

var (
	snowflakeQuerySQL    string
	snowflakeQueryFormat string
	snowflakeQueryOutput string
)

var snowflakeQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Run a SQL query against a registered Snowflake account",
	Long: `Execute SQL using Red Hat SSO OAuth tokens stored by finops config account add snowflake.

Examples:
  finops snowflake query --sql "SELECT CURRENT_USER(), CURRENT_ROLE()"
  finops snowflake query --account-alias sandbox --sql "SELECT 1"
  finops snowflake query --sql "SELECT 1" --format json`,
	RunE: runSnowflakeQuery,
}

func init() {
	snowflakeCmd.AddCommand(snowflakeQueryCmd)
	snowflakeQueryCmd.Flags().StringVar(&snowflakeQuerySQL, "sql", "", "SQL statement to execute (required)")
	snowflakeQueryCmd.Flags().StringVar(&snowflakeQueryFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	addOutputFlag(snowflakeQueryCmd, &snowflakeQueryOutput)
	_ = snowflakeQueryCmd.MarkFlagRequired("sql")
}

func runSnowflakeQuery(cmd *cobra.Command, _ []string) error {
	format, err := output.ParseFormat(snowflakeQueryFormat)
	if err != nil {
		return err
	}

	path, err := configstore.ResolvePath(snowflakeFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		return err
	}

	alias, acct, err := cfg.ResolveSnowflakeAccountAlias(snowflakeFlags.AccountAlias)
	if err != nil {
		return err
	}
	acct = cfg.ResolveSnowflakeSession(acct)
	if err := configstore.ValidateSnowflakeWarehouse(acct, alias); err != nil {
		return err
	}

	tok, oauthCfg, err := ensureSnowflakeAccessToken(cmd.Context(), cfg, alias, snowflakeFlags.SecretsPath, snowflakeFlags.TokensPath, acct)
	if err != nil {
		return err
	}

	claims, err := snowflakeoauth.ValidateDataverseToken(tok.AccessToken, oauthCfg.Audience)
	if err != nil {
		return err
	}

	db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
		Account:   acct.Account,
		User:      claims.SnowflakeLoginName(),
		Token:     tok.AccessToken,
		Role:      acct.Role,
		Warehouse: acct.Warehouse,
		Database:  acct.Database,
		Schema:    acct.Schema,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	result, err := coresnowflake.Query(cmd.Context(), db, snowflakeQuerySQL)
	if err != nil {
		return err
	}

	out, closeOut, err := resolveCommandOutput(cmd, snowflakeQueryOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}
	return output.WriteSnowflakeQueryResult(out, format, result)
}
