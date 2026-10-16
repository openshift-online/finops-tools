// extendedsupport_aws.go prepares AWS scan targets for extended-support discovery.
package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/awslogin"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/openshift-online/finops-tools/core/parallel"
	"github.com/spf13/cobra"
)

var (
	ensureExtendedSupportCredentials = ensureSnapshotCredentialsImpl
	prepareExtendedSupportTargets    = prepareExtendedSupportTargetsImpl
	newExtendedSupportPayerSession   = func(options awsauth.EnsureOptions) extendedSupportPayerSession {
		return awsauth.NewPayerSession(options)
	}
)

type extendedSupportPayerSession interface {
	Config(context.Context) (aws.Config, error)
	LinkedConfig(context.Context, string, string) (aws.Config, error)
}

func prepareExtendedSupportTargetsImpl(
	cmd *cobra.Command,
	cfg configstore.File,
	targets []cost.AccountTarget,
	credentialsFile, configPath, flagRole string,
	workers int,
	bar *progress.Bar,
) ([]extendedsupport.AccountTarget, []extendedsupport.AccountWarning, error) {
	ctx := awsCommandContext(cmd)
	if bar != nil {
		defer bar.Finish()
	}
	payers := make(map[string]extendedSupportPayerSession)
	for _, target := range targets {
		payerID := target.CredentialsAccountID()
		if _, ok := payers[payerID]; ok {
			continue
		}
		options, err := newAWSEnsureOptions(cmd, awsEnsureConfig{
			configPath: configPath, credentialsFile: credentialsFile, authMethodFlag: awsFlags.AuthMethod,
		})
		if err != nil {
			return nil, nil, err
		}
		options.AccountName = payerID
		options.ProfileNames = account.AWSProfileNames(payerID, cfg.PayerAliasForAccountID(payerID), nil)
		if options.Method == awsauth.MethodSAML {
			options.Provider = awslogin.SAMLLoginRunner{NoticeWriter: cmd.ErrOrStderr()}
		}
		payers[payerID] = newExtendedSupportPayerSession(options)
	}
	out := make([]extendedsupport.AccountTarget, len(targets))
	err := parallel.ForEach(ctx, workers, len(targets), func(ctx context.Context, i int) error {
		target := targets[i]
		accountID := strings.TrimSpace(target.AccountID)
		if accountID == "" {
			return fmt.Errorf("account target %d: account ID is required", i+1)
		}
		prepared := extendedsupport.AccountTarget{
			AccountID:    accountID,
			DisplayName:  target.AccountDisplayName(),
			DisplayAlias: target.DisplayAlias,
		}
		payer := payers[target.CredentialsAccountID()]
		if target.IsLinked() {
			// Resolve the role now, but authenticate only when Fetch starts this account.
			roleARN, err := resolveTargetLinkedRoleARN(cmd, cfg, configPath, accountID, target.DisplayAlias, flagRole)
			if err != nil {
				return err
			}
			prepared.ConfigLoader = func(ctx context.Context) (aws.Config, error) {
				return payer.LinkedConfig(ctx, accountID, roleARN)
			}
		} else {
			prepared.ConfigLoader = payer.Config
		}
		out[i] = prepared
		if bar != nil {
			bar.Advance()
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AccountID != out[j].AccountID {
			return out[i].AccountID < out[j].AccountID
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out, nil, nil
}

func mergeExtendedSupportSkippedAccounts(prepare, scan []extendedsupport.AccountWarning) []extendedsupport.AccountWarning {
	if len(prepare) == 0 && len(scan) == 0 {
		return nil
	}
	byAccount := make(map[string]extendedsupport.AccountWarning, len(prepare)+len(scan))
	for _, warning := range prepare {
		byAccount[warning.AccountID] = warning
	}
	for _, warning := range scan {
		byAccount[warning.AccountID] = warning
	}
	out := make([]extendedsupport.AccountWarning, 0, len(byAccount))
	for _, warning := range byAccount {
		out = append(out, warning)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AccountID != out[j].AccountID {
			return out[i].AccountID < out[j].AccountID
		}
		return out[i].Message < out[j].Message
	})
	return out
}
