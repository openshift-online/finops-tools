// aws_migrate_account.go implements "finops aws migrate-account".
package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/account"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/awsrole"
	"github.com/openshift-online/finops-tools/cli/internal/cache"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/spf13/cobra"
)

var (
	migrateAccountEnsureCredentials  = awsauth.EnsureAccountCredentials
	migrateAccountLoadConfigForCreds = loadAWSConfigForCredentialsAccount
	migrateAccountAssumeLinked       = awsconfig.AssumeLinkedCredentials
	migrateAccountLoadFromSession    = awsconfig.LoadConfigFromSession
	migrateAccountContains           = coreaccount.OrganizationContainsAccount
	migrateAccountInvite             = coreaccount.InviteAccount
	migrateAccountAccept             = coreaccount.AcceptInviteHandshake
	migrateAccountMove               = coreaccount.MoveAccountToParent
	migrateAccountUpdateConfig       = configstore.UpdateLinkedAccountPayer
	migrateAccountInvalidateOrgCache = invalidateOrgCacheForPayer

	migrateAccountIDFlag    string
	migrateAccountFromPayer string
	migrateAccountToPayer   string
	migrateAccountDestOU    string
	migrateAccountRole      string
	migrateAccountNotes     string
	migrateAccountDryRun    bool
	migrateAccountYes       bool
)

var migrateDestinationParentPattern = regexp.MustCompile(`^(ou-[0-9a-z]{4,32}-[0-9a-z]{4,32}|r-[0-9a-z]{4,32})$`)

var awsMigrateAccountCmd = &cobra.Command{
	Use:   "migrate-account",
	Short: "Move a linked AWS account from one payer organization to another",
	Long: `Invite a member account into a destination payer organization and accept the handshake.

Uses AWS Organizations direct transfer (invite from destination management account,
accept from the member account via role assumption from the source payer).

Requires management-account admin on both payers and OrganizationAccountAccessRole
(or --role) in the member account. SCPs or Control Tower may still block the transfer.

Examples:
  finops aws migrate-account --account-id 111111111111 --from-payer rh-control --to-payer osd-staging-1 --dry-run
  finops aws migrate-account --account-id 111111111111,222222222222 --from-payer rh-control --to-payer osd-staging-1 --yes
  finops aws migrate-account --account-id 111111111111 --from-payer rh-control --to-payer osd-staging-1 --destination-ou ou-abcd-12345678 --yes`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(migrateAccountFromPayer) == "" {
			return fmt.Errorf("--from-payer is required")
		}
		if strings.TrimSpace(migrateAccountToPayer) == "" {
			return fmt.Errorf("--to-payer is required")
		}
		if strings.TrimSpace(migrateAccountFromPayer) == strings.TrimSpace(migrateAccountToPayer) {
			return fmt.Errorf("--from-payer and --to-payer must differ")
		}
		if _, err := configstore.ParseAWSAccountIDs(migrateAccountIDFlag); err != nil {
			return fmt.Errorf("--account-id: %w", err)
		}
		if dest := strings.TrimSpace(migrateAccountDestOU); dest != "" && !migrateDestinationParentPattern.MatchString(dest) {
			return fmt.Errorf("invalid --destination-ou %q (expected ou-xxxx-yyyyy or r-xxxx)", dest)
		}
		if cmd.Flags().Changed("role") {
			if _, err := resolveLinkedRoleName(cmd, awsFlags.ConfigPath, migrateAccountRole); err != nil {
				return err
			}
		}
		if !migrateAccountDryRun && !migrateAccountYes {
			return fmt.Errorf("refusing to migrate without --yes (or pass --dry-run)")
		}
		return nil
	},
	RunE: runAWSMigrateAccount,
}

func init() {
	awsCmd.AddCommand(awsMigrateAccountCmd)
	awsMigrateAccountCmd.Flags().StringVar(&migrateAccountIDFlag, "account-id", "", "Linked account ID(s), comma-separated 12-digit IDs (required)")
	awsMigrateAccountCmd.Flags().StringVar(&migrateAccountFromPayer, "from-payer", "", "Source payer alias (required)")
	awsMigrateAccountCmd.Flags().StringVar(&migrateAccountToPayer, "to-payer", "", "Destination payer alias (required)")
	awsMigrateAccountCmd.Flags().StringVar(&migrateAccountDestOU, "destination-ou", "", "Optional destination OU or root ID after accept")
	awsMigrateAccountCmd.Flags().StringVar(&migrateAccountRole, "role", "", "IAM role name in the member account (default: linked config role or aws.linked_role)")
	awsMigrateAccountCmd.Flags().StringVar(&migrateAccountNotes, "notes", "", "Optional invitation notes")
	awsMigrateAccountCmd.Flags().BoolVar(&migrateAccountDryRun, "dry-run", false, "Read-only validation and plan (requires valid payer credentials; may call Organizations APIs to inspect account state; no mutations)")
	awsMigrateAccountCmd.Flags().BoolVar(&migrateAccountYes, "yes", false, "Confirm the migration (required unless --dry-run)")
}

func runAWSMigrateAccount(cmd *cobra.Command, _ []string) error {
	configPath, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(configPath)
	if err != nil {
		return err
	}

	fromAlias := strings.TrimSpace(migrateAccountFromPayer)
	toAlias := strings.TrimSpace(migrateAccountToPayer)
	fromPayerID, ok := cfg.PayerAccountIDForAlias(fromAlias)
	if !ok {
		return errUnknownPayerAlias(fromAlias)
	}
	toPayerID, ok := cfg.PayerAccountIDForAlias(toAlias)
	if !ok {
		return errUnknownPayerAlias(toAlias)
	}
	if fromPayerID == toPayerID {
		return fmt.Errorf("--from-payer %q and --to-payer %q resolve to the same payer account ID %s", fromAlias, toAlias, fromPayerID)
	}

	accountIDs, err := configstore.ParseAWSAccountIDs(migrateAccountIDFlag)
	if err != nil {
		return fmt.Errorf("--account-id: %w", err)
	}

	awsCtx := awsCommandContext(cmd)
	if err := ensureMigratePayerCredentials(cmd, awsCtx, fromAlias, fromPayerID); err != nil {
		return err
	}
	if err := ensureMigratePayerCredentials(cmd, awsCtx, toAlias, toPayerID); err != nil {
		return err
	}

	fromCfg, err := migrateAccountLoadConfigForCreds(awsCtx, cfg, fromPayerID, awsFlags.CredentialsFile)
	if err != nil {
		return err
	}
	toCfg, err := migrateAccountLoadConfigForCreds(awsCtx, cfg, toPayerID, awsFlags.CredentialsFile)
	if err != nil {
		return err
	}

	for i, accountID := range accountIDs {
		if len(accountIDs) > 1 {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "=== Account %d/%d: %s ===\n", i+1, len(accountIDs), accountID)
		}
		if err := migrateOneAccount(cmd, awsCtx, cfg, configPath, accountID, fromAlias, toAlias, fromPayerID, toPayerID, fromCfg, toCfg); err != nil {
			return err
		}
	}
	return nil
}

func migrateOneAccount(
	cmd *cobra.Command,
	awsCtx context.Context,
	cfg configstore.File,
	configPath, accountID, fromAlias, toAlias, fromPayerID, toPayerID string,
	fromCfg, toCfg aws.Config,
) error {
	accountAlias, roleName, err := resolveMigrateAccountTarget(cmd, cfg, configPath, accountID)
	if err != nil {
		return err
	}

	inSource, err := migrateAccountContains(awsCtx, fromCfg, accountID)
	if err != nil {
		return fmt.Errorf("verify membership in source payer %s: %w", fromAlias, err)
	}
	if !inSource {
		return fmt.Errorf("account %s is not an active member of source payer %s", accountID, fromAlias)
	}
	inDest, err := migrateAccountContains(awsCtx, toCfg, accountID)
	if err != nil {
		return fmt.Errorf("verify membership in destination payer %s: %w", toAlias, err)
	}
	if inDest {
		return fmt.Errorf("account %s is already a member of destination payer %s", accountID, toAlias)
	}

	destOU := strings.TrimSpace(migrateAccountDestOU)
	displayAccount := accountID
	if accountAlias != "" && accountAlias != accountID {
		displayAccount = fmt.Sprintf("%s (%s)", accountAlias, accountID)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Plan:\n")
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  account:          %s\n", displayAccount)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  from payer:       %s (%s)\n", fromAlias, fromPayerID)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  to payer:         %s (%s)\n", toAlias, toPayerID)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  assume role:      %s\n", roleName)
	if destOU != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  destination OU:   %s\n", destOU)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  steps:            invite (to-payer) → assume role (from-payer) → accept handshake")
	if destOU != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), " → move to OU")
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), " → update local config\n")

	if migrateAccountDryRun {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Dry run complete; no changes made.")
		return nil
	}

	invite, err := migrateAccountInvite(awsCtx, toCfg, accountID, strings.TrimSpace(migrateAccountNotes))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Invited account; handshake %s (state %s)\n", invite.HandshakeID, invite.State)

	roleARN, err := awsrole.LinkedRoleARN(accountID, roleName)
	if err != nil {
		return err
	}
	linkedSess, _, err := migrateAccountAssumeLinked(awsCtx, awsconfig.EnsureLinkedOptions{
		PayerAccountID:    fromPayerID,
		PayerProfileNames: account.AWSProfileNames(fromPayerID, fromAlias, nil),
		LinkedAccountID:   accountID,
		RoleARN:           roleARN,
		CredentialsPath:   awsFlags.CredentialsFile,
	})
	if err != nil {
		return fmt.Errorf("assume role into member account %s from payer %s: %w", accountID, fromAlias, err)
	}
	memberCfg, err := migrateAccountLoadFromSession(awsCtx, linkedSess)
	if err != nil {
		return fmt.Errorf("load member AWS config: %w", err)
	}

	accepted, err := migrateAccountAccept(awsCtx, memberCfg, invite.HandshakeID)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Accepted handshake %s (state %s)\n", accepted.HandshakeID, accepted.State)

	if destOU != "" {
		if err := migrateAccountMove(awsCtx, toCfg, accountID, destOU); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Moved account to %s\n", destOU)
	}

	if err := migrateAccountUpdateConfig(configPath, preferNonEmpty(accountAlias, accountID), toAlias, roleName); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: AWS migrate succeeded but local config update failed: %v\n", err)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated local linked account payer_alias to %s\n", toAlias)
	}

	_ = migrateAccountInvalidateOrgCache(configPath, fromPayerID)
	_ = migrateAccountInvalidateOrgCache(configPath, toPayerID)

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Migration complete.")
	return nil
}

func ensureMigratePayerCredentials(cmd *cobra.Command, ctx context.Context, alias, payerID string) error {
	profiles := account.AWSProfileNames(payerID, alias, nil)
	ensureOpts, err := newAWSEnsureOptions(cmd, awsEnsureConfig{
		configPath:      awsFlags.ConfigPath,
		authMethodFlag:  awsFlags.AuthMethod,
		credentialsFile: awsFlags.CredentialsFile,
	})
	if err != nil {
		return err
	}
	ensureOpts.AccountName = payerID
	ensureOpts.ProfileNames = profiles
	if _, err := migrateAccountEnsureCredentials(ctx, ensureOpts); err != nil {
		return fmt.Errorf("%s: %w", payerID, mapCredentialError(payerID, err))
	}
	return nil
}

// resolveMigrateAccountTarget resolves display alias and assume-role name for a validated account ID.
func resolveMigrateAccountTarget(cmd *cobra.Command, cfg configstore.File, configPath, accountID string) (alias, roleName string, err error) {
	alias = cfg.AliasForAccountID(accountID)
	if linked, ok := cfg.LinkedAccountForAlias(alias); ok && linked.AccountID == accountID {
		if !cmd.Flags().Changed("role") {
			if name := linked.RoleName(); name != "" {
				return alias, name, nil
			}
		}
	}
	roleName, err = resolveLinkedRoleName(cmd, configPath, migrateAccountRole)
	return alias, roleName, err
}

func preferNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func invalidateOrgCacheForPayer(configPath, payerID string) error {
	payerID = strings.TrimSpace(payerID)
	if payerID == "" {
		return nil
	}
	return cache.New(configPath).Delete("org", payerID)
}
