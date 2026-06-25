// snapshot_aws.go ensures AWS credentials for each snapshot scan target account.
package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/snapshot"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

var (
	ensureSnapshotCredentials = ensureSnapshotCredentialsImpl
	prepareSnapshotTargets    = prepareSnapshotTargetsImpl
	ensureSnapshotLinked      = awsconfig.EnsureLinkedCredentials
)

func ensureSnapshotCredentialsImpl(
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

func prepareSnapshotTargetsImpl(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	targets []cost.AccountTarget,
	credentialsFile, configPath, flagRole string,
	status costStepper,
) ([]snapshot.AccountTarget, error) {
	configCache := make(map[string]aws.Config)
	out := make([]snapshot.AccountTarget, 0, len(targets))

	for i := range targets {
		reportPrepareProgress(status, i+1, len(targets))
		target := targets[i]
		accountID := strings.TrimSpace(target.AccountID)
		if accountID == "" {
			return nil, fmt.Errorf("account target %d: account ID is required", i+1)
		}

		awsCfg, err := awsConfigForSnapshotTarget(ctx, cmd, cfg, target, credentialsFile, configPath, flagRole, configCache)
		if err != nil {
			return nil, err
		}

		bt := snapshot.AccountTarget{
			AccountID:    accountID,
			DisplayAlias: target.DisplayAlias,
			AWSConfig:    awsCfg,
		}
		if err := enrichSnapshotTargetDisplayName(ctx, &bt, cfg, target); err != nil {
			return nil, err
		}
		out = append(out, bt)
	}
	return out, nil
}

func awsConfigForSnapshotTarget(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	target cost.AccountTarget,
	credentialsFile, configPath, flagRole string,
	configCache map[string]aws.Config,
) (aws.Config, error) {
	if target.IsLinked() {
		return linkedAWSConfigForSnapshotTarget(ctx, cmd, cfg, target, credentialsFile, configPath, flagRole, configCache)
	}

	credID := target.CredentialsAccountID()
	if cached, ok := configCache[credID]; ok {
		return cached, nil
	}
	awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, credID, credentialsFile)
	if err != nil {
		return aws.Config{}, err
	}
	configCache[credID] = awsCfg
	return awsCfg, nil
}

func linkedAWSConfigForSnapshotTarget(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	target cost.AccountTarget,
	credentialsFile, configPath, flagRole string,
	configCache map[string]aws.Config,
) (aws.Config, error) {
	accountID := strings.TrimSpace(target.AccountID)
	payerID := target.CredentialsAccountID()

	if _, err := cachedOrLoadConfig(ctx, cfg, payerID, credentialsFile, configCache); err != nil {
		return aws.Config{}, err
	}

	roleARN, err := resolveSnapshotLinkedRoleARN(cmd, cfg, target, flagRole)
	if err != nil {
		return aws.Config{}, err
	}

	payerAlias := cfg.PayerAliasForAccountID(payerID)
	res, err := ensureSnapshotLinked(ctx, awsconfig.EnsureLinkedOptions{
		PayerAccountID:     payerID,
		LinkedAccountID:    accountID,
		RoleARN:            roleARN,
		CredentialsPath:    credentialsFile,
		PayerProfileNames:  account.AWSProfileNames(payerID, payerAlias, nil),
		LinkedProfileNames: account.AWSProfileNames(accountID, target.DisplayAlias, nil),
	})
	if err != nil {
		return aws.Config{}, fmt.Errorf("%s: %w", accountID, err)
	}

	profile := res.Profile
	if profile == "" {
		profile = awsconfig.SanitizeProfileName(accountID)
	}
	awsCfg, err := awsconfig.LoadSharedConfigProfile(ctx, profile)
	if err != nil {
		return aws.Config{}, fmt.Errorf("%s: load linked profile %q: %w", accountID, profile, err)
	}
	configCache[accountID] = awsCfg
	return awsCfg, nil
}

func cachedOrLoadConfig(
	ctx context.Context,
	cfg configstore.File,
	accountID, credentialsFile string,
	configCache map[string]aws.Config,
) (aws.Config, error) {
	if cached, ok := configCache[accountID]; ok {
		return cached, nil
	}
	awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, accountID, credentialsFile)
	if err != nil {
		return aws.Config{}, err
	}
	configCache[accountID] = awsCfg
	return awsCfg, nil
}

func resolveSnapshotLinkedRoleARN(cmd *cobra.Command, cfg configstore.File, target cost.AccountTarget, flagRole string) (string, error) {
	if alias := strings.TrimSpace(target.DisplayAlias); alias != "" {
		if linked, ok := cfg.LinkedAccountForAlias(alias); ok {
			return cfg.LinkedRoleARNForAccount(linked.AccountID, linked.RoleName())
		}
	}
	return resolveLinkedRoleARN(cmd, awsFlags.ConfigPath, target.AccountID, flagRole)
}

func enrichSnapshotTargetDisplayName(
	ctx context.Context,
	target *snapshot.AccountTarget,
	store configstore.File,
	source cost.AccountTarget,
) error {
	if strings.TrimSpace(target.DisplayName) != "" {
		return nil
	}
	if name := strings.TrimSpace(source.DisplayName); name != "" {
		target.DisplayName = name
		return nil
	}

	ct := cost.AccountTarget{
		AccountID:      target.AccountID,
		PayerAccountID: source.PayerAccountID,
		AWSConfig:      target.AWSConfig,
		DisplayAlias:   source.DisplayAlias,
	}
	if err := enrichCostTargetDisplayName(ctx, &ct, store); err != nil {
		return err
	}
	target.DisplayName = ct.DisplayName
	if target.DisplayName == "" {
		name, err := coreaccount.AccountName(ctx, target.AWSConfig, target.AccountID)
		if err == nil {
			target.DisplayName = name
		}
	}
	return nil
}
