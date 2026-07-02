// tag_add.go implements "finops tag add" for AWS Organizations account tags.
package cmd

import (
	"fmt"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/spf13/cobra"
)

var tagAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add an AWS Organizations tag to an account",
	Long: `Add one AWS Organizations tag to an account.

Pass either --account-alias or --account-id.

By default, the command fails when the tag key already exists.
Use --force to overwrite an existing tag value.

Examples:
  finops tag add --account-alias rh-control --tag-key owner --tag-value team-a
  finops tag add --account-alias osd-tenant-1 --tag-key env --tag-value prod --force
  finops tag add --account-id 111111111111 --tag-key env --tag-value prod --payer rh-control`,
	Args: cobra.NoArgs,
	RunE: runTagAdd,
}

var (
	accountAddTagKey                 string
	accountAddTagValue               string
	accountAddTagForce               bool
	accountAddTagPayer               string
	accountAddTagAlias               string
	accountAddTagAccountID           string
	accountAddTagEnsureCredentialsFn = awsauth.EnsureAccountCredentials
	accountAddTagLoadConfigFn        = loadAWSConfigForCredentialsAccount
	accountAddTagListTagsFn          = coreaccount.ListTags
	accountAddTagSetAccountTagFn     = coreaccount.SetAccountTag
	accountAddTagDetectKindFn        = coreaccount.DetectAccountKind
)

func init() {
	tagCmd.AddCommand(tagAddCmd)
	tagAddCmd.Flags().StringVar(&accountAddTagKey, "tag-key", "", "Tag key")
	tagAddCmd.Flags().StringVar(&accountAddTagValue, "tag-value", "", "Tag value")
	tagAddCmd.Flags().BoolVar(&accountAddTagForce, "force", false, "Overwrite existing value when the tag key already exists")
	bindAWSAccountSelectorFlags(tagAddCmd, awsAccountSelectorFlagRefs{
		Payer:     &accountAddTagPayer,
		Alias:     &accountAddTagAlias,
		AccountID: &accountAddTagAccountID,
	}, "Registered payer alias to use for credentials when mutating account tags")
}

func runTagAdd(cmd *cobra.Command, args []string) error {
	tagKey := strings.TrimSpace(accountAddTagKey)
	if tagKey == "" {
		return fmt.Errorf("tag key is required (--tag-key)")
	}
	tagValue := strings.TrimSpace(accountAddTagValue)
	if tagValue == "" {
		return fmt.Errorf("tag value is required (--tag-value)")
	}

	configPath, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(configPath)
	if err != nil {
		return err
	}
	target, err := resolveAccountTagsTargetExplicit(cfg, accountAddTagAlias, accountAddTagAccountID)
	if err != nil {
		return err
	}
	if payerAlias := strings.TrimSpace(accountAddTagPayer); payerAlias != "" {
		payerID, ok := cfg.PayerAccountIDForAlias(payerAlias)
		if !ok {
			return errUnknownPayerAlias(payerAlias)
		}
		target.CredentialsAccountID = payerID
	} else if target.CredentialsAccountID == target.AccountID && cfg.PayerAliasForAccountID(target.CredentialsAccountID) == "" {
		return fmt.Errorf("account tag mutations require payer credentials; account %s is not mapped to a payer (use --payer <payer-alias>)", target.AccountID)
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
	if _, err := accountAddTagEnsureCredentialsFn(awsCtx, ensureOpts); err != nil {
		return fmt.Errorf("%s: %w", target.CredentialsAccountID, mapCredentialError(target.CredentialsAccountID, err))
	}

	awsCfg, err := accountAddTagLoadConfigFn(awsCtx, cfg, target.CredentialsAccountID, awsFlags.CredentialsFile)
	if err != nil {
		return err
	}
	kind, err := accountAddTagDetectKindFn(awsCtx, awsCfg, target.CredentialsAccountID)
	if err != nil {
		return fmt.Errorf("account tag mutations require payer credentials; unable to verify account %s is a payer: %w", target.CredentialsAccountID, err)
	}
	if kind != coreaccount.AccountKindPayer {
		return fmt.Errorf("account tag mutations require payer credentials; account %s is %s (use --payer <payer-alias>)", target.CredentialsAccountID, kind)
	}

	tags, err := accountAddTagListTagsFn(awsCtx, awsCfg, target.AccountID)
	if err != nil {
		return fmt.Errorf("list tags for account %s: %w", target.AccountID, err)
	}
	if accountHasTagKey(tags, tagKey) && !accountAddTagForce {
		return fmt.Errorf("tag %q already exists on account %s (use --force to overwrite)", tagKey, target.AccountID)
	}
	if err := accountAddTagSetAccountTagFn(awsCtx, awsCfg, target.AccountID, tagKey, tagValue); err != nil {
		return fmt.Errorf("add tag %q on account %s: %w", tagKey, target.AccountID, err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Tag %q added on account %s with value %q.\n", tagKey, accountTagTargetLabel(target), tagValue)
	return err
}

func accountHasTagKey(tags []coreaccount.Tag, tagKey string) bool {
	for _, tag := range tags {
		if tag.Key == tagKey {
			return true
		}
	}
	return false
}

func accountTagTargetLabel(target accountTagsTarget) string {
	if target.Alias == "" || target.Alias == target.AccountID {
		return target.AccountID
	}
	return fmt.Sprintf("%s (%s)", target.Alias, target.AccountID)
}
