// config_account_list.go implements "finops config account list" to show registered account aliases.
package cmd

import (
	"fmt"
	"io"
	"slices"

	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	accountListFormat string
	accountListOutput string
)

var accountListCmd = &cobra.Command{
	Use:   "list [provider]",
	Short: "List registered cloud accounts and aliases",
	Long: `List accounts saved in the finops config file.

AWS entries show whether each alias is a payer account (org billing / Cost Explorer)
or a linked member account (role assumption from a registered payer).

Examples:
  finops config account list
  finops config account list aws
  finops config account list gcp
  finops config account list snowflake`,
	Args: cobra.MaximumNArgs(1),
	PreRunE: func(_ *cobra.Command, _ []string) error {
		_, err := output.ParseFormat(accountListFormat)
		return err
	},
	RunE: runAccountList,
}

func init() {
	configAccountCmd.AddCommand(accountListCmd)
	accountListCmd.Flags().StringVar(&accountListFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	addOutputFlag(accountListCmd, &accountListOutput)
}

func runAccountList(cmd *cobra.Command, args []string) error {
	format, err := output.ParseFormat(accountListFormat)
	if err != nil {
		return err
	}
	provider := account.ProviderAWS
	if len(args) == 1 {
		p, err := account.ParseProvider(args[0])
		if err != nil {
			return err
		}
		provider = p
	}

	path, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		return err
	}

	out, closeOut, err := resolveCommandOutput(cmd, accountListOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}

	switch provider {
	case account.ProviderAWS:
		return printAWSAccountList(out, format, cfg)
	case account.ProviderGCP:
		return printGCPAccountList(out, format, cfg)
	case account.ProviderSnowflake:
		return printSnowflakeAccountList(out, format, cfg)
	default:
		return fmt.Errorf("unsupported provider %q", provider)
	}
}

func printAWSAccountList(w io.Writer, format output.Format, cfg configstore.File) error {
	rows := awsAccountListRows(cfg.ListAWSAccounts())
	return output.WriteAccountListResult(w, format, "aws", rows)
}

func printGCPAccountList(w io.Writer, format output.Format, cfg configstore.File) error {
	aliases := make([]string, 0, len(cfg.GCP.AccountAliases))
	for alias := range cfg.GCP.AccountAliases {
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)

	rows := make([]output.AccountListRow, 0, len(aliases))
	for _, alias := range aliases {
		rows = append(rows, output.AccountListRow{
			Alias:     alias,
			AccountID: cfg.GCP.AccountAliases[alias],
		})
	}
	return output.WriteAccountListResult(w, format, "gcp", rows)
}

func printSnowflakeAccountList(w io.Writer, format output.Format, cfg configstore.File) error {
	entries := cfg.ListSnowflakeAccounts()
	rows := make([]output.AccountListRow, len(entries))
	for i, e := range entries {
		rows[i] = output.AccountListRow{
			Alias:     e.Alias,
			AccountID: e.Account,
			Kind:      "snowflake",
			Role:      e.Role,
		}
	}
	return output.WriteAccountListResult(w, format, "snowflake", rows)
}

func awsAccountListRows(entries []configstore.AWSAccountListEntry) []output.AccountListRow {
	rows := make([]output.AccountListRow, len(entries))
	for i, e := range entries {
		rows[i] = output.AccountListRow{
			Alias:      e.Alias,
			AccountID:  e.AccountID,
			Kind:       e.Kind,
			PayerAlias: e.PayerAlias,
			Role:       e.Role,
		}
	}
	return rows
}
