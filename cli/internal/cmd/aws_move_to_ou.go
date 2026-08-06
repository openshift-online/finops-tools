// aws_move_to_ou.go implements "finops aws move-to-ou".
package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/spf13/cobra"
)

var (
	moveToOUEnsureCredentials  = awsauth.EnsureAccountCredentials
	moveToOULoadConfigForCreds = loadAWSConfigForCredentialsAccount
	moveToOUContains           = coreaccount.OrganizationContainsAccount
	moveToOUParentID           = coreaccount.AccountParentID
	moveToOUVerifyParent       = coreaccount.VerifyParentExists
	moveToOUMove               = coreaccount.MoveAccountToParent
	moveToOUInvalidateOrgCache = invalidateOrgCacheForPayer

	moveToOUAccountIDFlag string
	moveToOUPayer         string
	moveToOUDestOU        string
	moveToOUDryRun        bool
	moveToOUYes           bool
)

var awsMoveToOUCmd = &cobra.Command{
	Use:   "move-to-ou",
	Short: "Move a linked AWS account to another OU within the same payer",
	Long: `Move one or more member accounts from their current OU (or root) to a destination
OU or root within the same payer organization.

This is a same-payer Organizations MoveAccount operation. For cross-payer transfers,
use "finops aws migrate-account".

Requires Organizations admin on the payer. Moving an account changes inherited SCPs
and may affect Control Tower / StackSets targeted at OUs.

Examples:
  finops aws move-to-ou --payer rh-control --account-id 111111111111 --destination-ou ou-abcd-12345678 --dry-run
  finops aws move-to-ou --payer rh-control --account-id 111111111111,222222222222 --destination-ou ou-abcd-12345678 --yes
  finops aws move-to-ou --payer rh-control --account-id 111111111111 --destination-ou r-abcd --yes`,
	Args: cobra.NoArgs,
	PreRunE: func(_ *cobra.Command, _ []string) error {
		if strings.TrimSpace(moveToOUPayer) == "" {
			return fmt.Errorf("--payer is required")
		}
		if _, err := configstore.ParseAWSAccountIDs(moveToOUAccountIDFlag); err != nil {
			return fmt.Errorf("--account-id: %w", err)
		}
		dest := strings.TrimSpace(moveToOUDestOU)
		if dest == "" {
			return fmt.Errorf("--destination-ou is required")
		}
		if err := coreaccount.ValidateParentID(dest); err != nil {
			return fmt.Errorf("invalid --destination-ou: %w", err)
		}
		if !moveToOUDryRun && !moveToOUYes {
			return fmt.Errorf("refusing to move without --yes (or pass --dry-run)")
		}
		return nil
	},
	RunE: runAWSMoveToOU,
}

func init() {
	awsCmd.AddCommand(awsMoveToOUCmd)
	awsMoveToOUCmd.Flags().StringVar(&moveToOUAccountIDFlag, "account-id", "", "Linked account ID(s), comma-separated 12-digit IDs (required)")
	awsMoveToOUCmd.Flags().StringVar(&moveToOUPayer, "payer", "", "Registered payer alias (required)")
	awsMoveToOUCmd.Flags().StringVar(&moveToOUDestOU, "destination-ou", "", "Destination OU or root ID (required)")
	awsMoveToOUCmd.Flags().BoolVar(&moveToOUDryRun, "dry-run", false, "Read-only validation and plan (no mutations)")
	awsMoveToOUCmd.Flags().BoolVar(&moveToOUYes, "yes", false, "Confirm the move (required unless --dry-run)")
}

func runAWSMoveToOU(cmd *cobra.Command, _ []string) error {
	configPath, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(configPath)
	if err != nil {
		return err
	}

	payerAlias := strings.TrimSpace(moveToOUPayer)
	payerID, ok := cfg.PayerAccountIDForAlias(payerAlias)
	if !ok {
		return errUnknownPayerAlias(payerAlias)
	}

	accountIDs, err := configstore.ParseAWSAccountIDs(moveToOUAccountIDFlag)
	if err != nil {
		return fmt.Errorf("--account-id: %w", err)
	}
	destOU := strings.TrimSpace(moveToOUDestOU)

	awsCtx := awsCommandContext(cmd)
	profiles := account.AWSProfileNames(payerID, payerAlias, nil)
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
	if _, err := moveToOUEnsureCredentials(awsCtx, ensureOpts); err != nil {
		return fmt.Errorf("%s: %w", payerID, mapCredentialError(payerID, err))
	}

	awsCfg, err := moveToOULoadConfigForCreds(awsCtx, cfg, payerID, awsFlags.CredentialsFile)
	if err != nil {
		return err
	}

	if err := moveToOUVerifyParent(awsCtx, awsCfg, destOU); err != nil {
		return fmt.Errorf("destination --destination-ou: %w", err)
	}

	var succeeded []string
	var movedIDs []string
	for i, accountID := range accountIDs {
		if len(accountIDs) > 1 {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "=== Account %d/%d: %s ===\n", i+1, len(accountIDs), accountID)
		}
		moved, err := moveOneAccountToOU(cmd, awsCtx, cfg, configPath, awsCfg, accountID, payerAlias, payerID, destOU)
		if err != nil {
			if len(accountIDs) == 1 {
				return err
			}
			return fmt.Errorf("%w%s", err, formatMigratePartialSummary(succeeded, accountIDs, i))
		}
		succeeded = append(succeeded, accountID)
		if moved {
			movedIDs = append(movedIDs, accountID)
		}
	}
	if len(accountIDs) > 1 && !moveToOUDryRun {
		if len(movedIDs) == 0 {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Processed %d/%d accounts; none required a move.\n", len(succeeded), len(accountIDs))
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Moved %d/%d accounts: %s\n", len(movedIDs), len(accountIDs), strings.Join(movedIDs, ", "))
		}
	}
	return nil
}

// moveOneAccountToOU plans and optionally moves one account. moved is true only when
// MoveAccountToParent ran successfully (false for dry-run and already-under-destination).
func moveOneAccountToOU(
	cmd *cobra.Command,
	awsCtx context.Context,
	cfg configstore.File,
	configPath string,
	awsCfg aws.Config,
	accountID, payerAlias, payerID, destOU string,
) (moved bool, err error) {
	if accountID == payerID {
		return false, fmt.Errorf("refusing to move management account %s (payer %s)", accountID, payerAlias)
	}

	inOrg, err := moveToOUContains(awsCtx, awsCfg, accountID)
	if err != nil {
		return false, fmt.Errorf("verify membership in payer %s: %w", payerAlias, err)
	}
	if !inOrg {
		return false, fmt.Errorf("account %s is not an active member of payer %s", accountID, payerAlias)
	}

	currentParent, err := moveToOUParentID(awsCtx, awsCfg, accountID)
	if err != nil {
		return false, err
	}

	displayAccount := accountID
	if alias := cfg.AliasForAccountID(accountID); alias != "" && alias != accountID {
		displayAccount = fmt.Sprintf("%s (%s)", alias, accountID)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Plan:\n")
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  account:          %s\n", displayAccount)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  payer:            %s (%s)\n", payerAlias, payerID)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  from:             %s\n", currentParent)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  to:               %s\n", destOU)

	if currentParent == destOU {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Account already under %s; nothing to do.\n", destOU)
		return false, nil
	}

	if moveToOUDryRun {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Dry run complete; no changes made.")
		return false, nil
	}

	if err := moveToOUMove(awsCtx, awsCfg, accountID, destOU); err != nil {
		return false, err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Moved account to %s\n", destOU)

	if err := moveToOUInvalidateOrgCache(configPath, payerID); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: moved account but failed to invalidate org cache for payer %s: %v\n", payerID, err)
	}
	return true, nil
}
