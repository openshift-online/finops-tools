// tag_list.go implements "finops tag list" to list AWS Organizations tags for one account.
package cmd

import (
	"fmt"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/spf13/cobra"
)

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List AWS Organizations tags for an account",
	Long: `List all AWS Organizations tags for an account.

Pass either --account-alias or --account-id.

Examples:
  finops tag list --account-alias rh-control
  finops tag list --account-alias osd-tenant-1 --format json
  finops tag list --account-id 123456789012 --format csv
  finops tag list --account-id 111111111111 --payer rh-control`,
	Args: cobra.NoArgs,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		_, err := output.ParseFormat(accountTagsFormat)
		return err
	},
	RunE: runTagList,
}

var (
	accountTagsEnsureCredentials  = awsauth.EnsureAccountCredentials
	accountTagsLoadConfigForCreds = loadAWSConfigForCredentialsAccount
	accountTagsFetch              = coreaccount.ListTags
	accountTagsFormat             string
	accountTagsOutput             string
	accountTagsPayer              string
	accountTagsAlias              string
	accountTagsAccountID          string
)

type accountTagsTarget struct {
	AccountID            string
	CredentialsAccountID string
	Alias                string
}

func init() {
	tagCmd.AddCommand(tagListCmd)
	tagListCmd.Flags().StringVar(&accountTagsFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	addOutputFlag(tagListCmd, &accountTagsOutput)
	bindAWSAccountSelectorFlags(tagListCmd, awsAccountSelectorFlagRefs{
		Payer:     &accountTagsPayer,
		Alias:     &accountTagsAlias,
		AccountID: &accountTagsAccountID,
	}, "Registered payer alias to use for credentials when listing account tags")
}

func runTagList(cmd *cobra.Command, args []string) error {
	format, err := output.ParseFormat(accountTagsFormat)
	if err != nil {
		return err
	}

	configPath, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(configPath)
	if err != nil {
		return err
	}

	target, err := resolveAccountTagsTargetExplicit(cfg, accountTagsAlias, accountTagsAccountID)
	if err != nil {
		return err
	}
	if payerAlias := strings.TrimSpace(accountTagsPayer); payerAlias != "" {
		payerID, ok := cfg.PayerAccountIDForAlias(payerAlias)
		if !ok {
			return errUnknownPayerAlias(payerAlias)
		}
		target.CredentialsAccountID = payerID
	}

	profiles := account.AWSProfileNames(
		target.CredentialsAccountID,
		cfg.PayerAliasForAccountID(target.CredentialsAccountID),
		nil,
	)

	ensureOpts, err := newAWSEnsureOptions(cmd, awsEnsureConfig{
		configPath:      awsFlags.ConfigPath,
		authMethodFlag:  awsFlags.AuthMethod,
		credentialsFile: awsFlags.CredentialsFile,
	})
	if err != nil {
		return err
	}
	ensureOpts.AccountName = target.CredentialsAccountID
	ensureOpts.ProfileNames = profiles
	awsCtx := awsCommandContext(cmd)
	if _, err := accountTagsEnsureCredentials(awsCtx, ensureOpts); err != nil {
		return fmt.Errorf("%s: %w", target.CredentialsAccountID, mapCredentialError(target.CredentialsAccountID, err))
	}

	awsCfg, err := accountTagsLoadConfigForCreds(awsCtx, cfg, target.CredentialsAccountID, awsFlags.CredentialsFile)
	if err != nil {
		return err
	}
	tags, err := accountTagsFetch(awsCtx, awsCfg, target.AccountID)
	if err != nil {
		return fmt.Errorf("list tags for account %s: %w", target.AccountID, err)
	}

	rows := make([]output.AccountTagRow, len(tags))
	for i, tag := range tags {
		rows[i] = output.AccountTagRow{
			Key:   tag.Key,
			Value: tag.Value,
		}
	}
	out, closeOut, err := resolveCommandOutput(cmd, accountTagsOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}
	return output.WriteAWSAccountTagsResult(out, format, output.AccountTagsView{
		AccountID: target.AccountID,
		Alias:     target.Alias,
		Tags:      rows,
	})
}

func resolveAccountTagsTargetExplicit(cfg configstore.File, accountAlias, accountID string) (accountTagsTarget, error) {
	accountAlias = strings.TrimSpace(accountAlias)
	accountID = strings.TrimSpace(accountID)
	if accountAlias != "" && accountID != "" {
		return accountTagsTarget{}, fmt.Errorf("provide exactly one of --account-alias or --account-id")
	}
	if accountAlias == "" && accountID == "" {
		return accountTagsTarget{}, fmt.Errorf("provide exactly one of --account-alias or --account-id")
	}

	if accountAlias != "" {
		if linked, ok := cfg.LinkedAccountForAlias(accountAlias); ok {
			payerID, ok := cfg.PayerAccountIDForAlias(linked.PayerAlias)
			if !ok {
				return accountTagsTarget{}, fmt.Errorf("unknown payer alias %q for linked account %q", linked.PayerAlias, accountAlias)
			}
			return accountTagsTarget{
				AccountID:            linked.AccountID,
				CredentialsAccountID: payerID,
				Alias:                accountAlias,
			}, nil
		}

		if payerID, ok := cfg.PayerAccountIDForAlias(accountAlias); ok {
			return accountTagsTarget{
				AccountID:            payerID,
				CredentialsAccountID: payerID,
				Alias:                accountAlias,
			}, nil
		}
		return accountTagsTarget{}, fmt.Errorf("unknown account alias %q", accountAlias)
	}

	if err := account.ValidateAWSAccountID(accountID); err != nil {
		return accountTagsTarget{}, err
	}

	credsID := accountID
	if payerID, ok := cfg.PayerAccountIDForLinkedAccountID(accountID); ok {
		credsID = payerID
	}

	alias := cfg.AliasForAccountID(accountID)
	if alias == accountID {
		alias = ""
	}

	return accountTagsTarget{
		AccountID:            accountID,
		CredentialsAccountID: credsID,
		Alias:                alias,
	}, nil
}
