// tag_update.go implements "finops tag update" for AWS Organizations account tags.
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

var tagUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update an AWS Organizations tag on an account",
	Long: `Update one AWS Organizations tag on an account.

Pass either --account-alias or --account-id.

By default, the command fails when the tag key does not exist.
Use --force to create the tag when it is missing.

Examples:
  finops tag update --account-alias rh-control --tag-key owner --tag-value team-b
  finops tag update --account-alias osd-tenant-1 --tag-key env --tag-value stage --force
  finops tag update --account-id 111111111111 --tag-key organization --tag-value "Hybrid Platform" --payer rh-control`,
	Args: cobra.NoArgs,
	RunE: runTagUpdate,
}

var (
	accountUpdateTagKey                 string
	accountUpdateTagValue               string
	accountUpdateTagForce               bool
	accountUpdateTagPayer               string
	accountUpdateTagAlias               string
	accountUpdateTagAccountID           string
	accountUpdateTagEnsureCredentialsFn = awsauth.EnsureAccountCredentials
	accountUpdateTagLoadConfigFn        = loadAWSConfigForCredentialsAccount
	accountUpdateTagListTagsFn          = coreaccount.ListTags
	accountUpdateTagSetAccountTagFn     = coreaccount.SetAccountTag
	accountUpdateTagDetectKindFn        = coreaccount.DetectAccountKind
)

func init() {
	tagCmd.AddCommand(tagUpdateCmd)
	tagUpdateCmd.Flags().StringVar(&accountUpdateTagKey, "tag-key", "", "Tag key")
	tagUpdateCmd.Flags().StringVar(&accountUpdateTagValue, "tag-value", "", "Tag value")
	tagUpdateCmd.Flags().BoolVar(&accountUpdateTagForce, "force", false, "Create the tag when the key does not already exist")
	bindAWSAccountSelectorFlags(tagUpdateCmd, awsAccountSelectorFlagRefs{
		Payer:     &accountUpdateTagPayer,
		Alias:     &accountUpdateTagAlias,
		AccountID: &accountUpdateTagAccountID,
	}, "Registered payer alias to use for credentials when mutating account tags")
}

func runTagUpdate(cmd *cobra.Command, args []string) error {
	tagKey := strings.TrimSpace(accountUpdateTagKey)
	if tagKey == "" {
		return fmt.Errorf("tag key is required (--tag-key)")
	}
	tagValue := strings.TrimSpace(accountUpdateTagValue)
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
	target, err := resolveAccountTagsTargetExplicit(cfg, accountUpdateTagAlias, accountUpdateTagAccountID)
	if err != nil {
		return err
	}
	if payerAlias := strings.TrimSpace(accountUpdateTagPayer); payerAlias != "" {
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
	if _, err := accountUpdateTagEnsureCredentialsFn(awsCtx, ensureOpts); err != nil {
		return fmt.Errorf("%s: %w", target.CredentialsAccountID, mapCredentialError(target.CredentialsAccountID, err))
	}

	awsCfg, err := accountUpdateTagLoadConfigFn(awsCtx, cfg, target.CredentialsAccountID, awsFlags.CredentialsFile)
	if err != nil {
		return err
	}
	kind, err := accountUpdateTagDetectKindFn(awsCtx, awsCfg, target.CredentialsAccountID)
	if err != nil {
		return fmt.Errorf("account tag mutations require payer credentials; unable to verify account %s is a payer: %w", target.CredentialsAccountID, err)
	}
	if kind != coreaccount.AccountKindPayer {
		return fmt.Errorf("account tag mutations require payer credentials; account %s is %s (use --payer <payer-alias>)", target.CredentialsAccountID, kind)
	}

	tags, err := accountUpdateTagListTagsFn(awsCtx, awsCfg, target.AccountID)
	if err != nil {
		return fmt.Errorf("list tags for account %s: %w", target.AccountID, err)
	}
	exists := accountHasTagKey(tags, tagKey)
	if !exists && !accountUpdateTagForce {
		return fmt.Errorf("tag %q does not exist on account %s (use --force to create it)", tagKey, target.AccountID)
	}
	if err := accountUpdateTagSetAccountTagFn(awsCtx, awsCfg, target.AccountID, tagKey, tagValue); err != nil {
		return fmt.Errorf("update tag %q on account %s: %w", tagKey, target.AccountID, err)
	}

	action := "updated"
	if !exists {
		action = "created"
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Tag %q %s on account %s with value %q.\n", tagKey, action, accountTagTargetLabel(target), tagValue)
	return err
}
