// cost_aws.go ensures AWS credentials for each cost target (payer and linked) before calling core/cost.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/account"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

var ensureCostCredentials = ensureCostCredentialsImpl

func ensureCostCredentialsImpl(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	targets []cost.AccountTarget,
	configPath, credentialsFile, authMethod string,
) error {
	seen := make(map[string]struct{})
	for i := range targets {
		credID := targets[i].CredentialsAccountID()
		if _, ok := seen[credID]; ok {
			continue
		}
		seen[credID] = struct{}{}

		ensureOpts, err := newAWSEnsureOptions(cmd, awsEnsureConfig{
			configPath:      configPath,
			authMethodFlag:  authMethod,
			credentialsFile: credentialsFile,
		})
		if err != nil {
			return err
		}
		ensureOpts.AccountName = credID
		ensureOpts.ProfileNames = account.AWSProfileNames(credID, cfg.PayerAliasForAccountID(credID), nil)

		if _, err := awsauth.EnsureAccountCredentials(ctx, ensureOpts); err != nil {
			return fmt.Errorf("%s: %w", credID, mapCredentialError(credID, err))
		}
	}
	return nil
}

func prepareCostTargets(
	ctx context.Context,
	store configstore.File,
	targets []cost.AccountTarget,
	credentialsFile string,
	status costStepper,
) ([]cost.AccountTarget, error) {
	credConfigs := make(map[string]aws.Config)
	total := len(targets)
	for i := range targets {
		reportPrepareProgress(status, i+1, total)
		credID := targets[i].CredentialsAccountID()
		if awsCfg, ok := credConfigs[credID]; ok {
			targets[i].AWSConfig = awsCfg
			if err := enrichCostTargetDisplayName(ctx, &targets[i], store); err != nil {
				return nil, err
			}
			continue
		}

		awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, store, credID, credentialsFile)
		if err != nil {
			return nil, err
		}
		credConfigs[credID] = awsCfg
		targets[i].AWSConfig = awsCfg

		if err := enrichCostTargetDisplayName(ctx, &targets[i], store); err != nil {
			return nil, err
		}
	}
	return targets, nil
}

func reportPrepareProgress(status costStepper, index, total int) {
	if status == nil || total <= 1 || !shouldReportIndexedProgress(index, total) {
		return
	}
	status.Step(fmt.Sprintf("Preparing account configuration (%d/%d)…", index, total))
}

var loadAWSConfigForCredentialsAccount = loadAWSConfigForCredentialsAccountImpl

func loadAWSConfigForCredentialsAccountImpl(
	ctx context.Context,
	store configstore.File,
	credentialsAccountID,
	credentialsFile string,
) (aws.Config, error) {
	profileNames := account.AWSProfileNames(credentialsAccountID, store.PayerAliasForAccountID(credentialsAccountID), nil)
	res, status, err := awsconfig.ResolveCredentials(ctx, awsconfig.ResolveOptions{
		AccountName:     credentialsAccountID,
		ProfileNames:    profileNames,
		CredentialsPath: credentialsFile,
	})
	if err != nil {
		return aws.Config{}, fmt.Errorf("%s: %w", credentialsAccountID, err)
	}
	if status != awsconfig.CredentialsValid {
		return aws.Config{}, fmt.Errorf("%s: %w", credentialsAccountID, mapCredentialStatusError(credentialsAccountID, status))
	}

	profile := res.Profile
	if profile == "" {
		profile = awsconfig.SanitizeProfileName(credentialsAccountID)
	}
	awsCfg, err := awsconfig.LoadSharedConfigProfile(ctx, profile)
	if err != nil {
		return aws.Config{}, fmt.Errorf("%s: load AWS profile %q: %w", credentialsAccountID, profile, err)
	}
	return awsCfg, nil
}

func enrichCostTargetDisplayName(
	ctx context.Context,
	target *cost.AccountTarget,
	store configstore.File,
) error {
	accountID := strings.TrimSpace(target.AccountID)
	if accountID == "" {
		return nil
	}
	if strings.TrimSpace(target.DisplayName) != "" {
		return nil
	}

	if name, err := coreaccount.AccountName(ctx, target.AWSConfig, accountID); err == nil && strings.TrimSpace(name) != "" {
		target.DisplayName = name
		return nil
	}

	lookupProfiles := account.AWSProfileNames(accountID, store.AliasForAccountID(accountID), nil)
	if payerID := target.PayerAccountID; payerID != "" {
		lookupProfiles = mergeProfileNames(
			lookupProfiles,
			account.AWSProfileNames(payerID, store.PayerAliasForAccountID(payerID), nil),
		)
	}

	for _, profile := range lookupProfiles {
		awsCfg, err := awsconfig.LoadSharedConfigProfile(ctx, profile)
		if err != nil {
			continue
		}
		name, err := coreaccount.AccountName(ctx, awsCfg, accountID)
		if err == nil {
			target.DisplayName = name
			return nil
		}
	}

	return nil
}

func mapCredentialError(accountID string, err error) error {
	if errors.Is(err, awsconfig.ErrCredentialsNotFound) {
		return fmt.Errorf("%w (run: finops config account add aws %s)", err, accountID)
	}
	if errors.Is(err, awsconfig.ErrCredentialsInvalid) {
		return fmt.Errorf("%w (run: finops config account add aws %s)", err, accountID)
	}
	return err
}

func mapCredentialStatusError(accountID string, status awsconfig.ResolveStatus) error {
	switch status {
	case awsconfig.CredentialsInvalid:
		return fmt.Errorf("%w (run: finops config account add aws %s)", awsconfig.ErrCredentialsInvalid, accountID)
	default:
		return fmt.Errorf("%w (run: finops config account add aws %s)", awsconfig.ErrCredentialsNotFound, accountID)
	}
}

func mergeProfileNames(segments ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, segment := range segments {
		for _, name := range segment {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}
