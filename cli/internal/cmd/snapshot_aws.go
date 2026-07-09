// snapshot_aws.go ensures AWS credentials for each snapshot scan target account.
package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/account"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/parallel"
	"github.com/openshift-online/finops-tools/core/snapshot"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

var (
	ensureSnapshotCredentials = ensureSnapshotCredentialsImpl
	prepareSnapshotTargets    = prepareSnapshotTargetsImpl
	assumeSnapshotLinked      = awsconfig.AssumeLinkedCredentials
	loadPayerProfileSessions  = loadPayerProfileSessionsImpl
)

func ensureSnapshotCredentialsImpl(
	cmd *cobra.Command,
	cfg configstore.File,
	targets []cost.AccountTarget,
	configPath, credentialsFile, authMethod string,
) error {
	ctx := awsCommandContext(cmd)
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
	cmd *cobra.Command,
	cfg configstore.File,
	targets []cost.AccountTarget,
	credentialsFile, configPath, flagRole string,
	workers int,
	bar *progress.Bar,
) ([]snapshot.AccountTarget, []snapshot.AccountWarning, error) {
	ctx := awsCommandContext(cmd)
	if bar != nil {
		defer bar.Finish()
	}
	payerSessions, err := loadPayerProfileSessions(ctx, cfg, targets, credentialsFile)
	if err != nil {
		return nil, nil, err
	}
	var (
		configMu sync.Mutex
		outMu    sync.Mutex
		skipMu   sync.Mutex
	)
	configCache := make(map[string]aws.Config)
	var out []snapshot.AccountTarget
	var skipped []snapshot.AccountWarning

	err = parallel.ForEach(ctx, workers, len(targets), func(ctx context.Context, i int) error {
		target := targets[i]
		accountID := strings.TrimSpace(target.AccountID)
		if accountID == "" {
			return fmt.Errorf("account target %d: account ID is required", i+1)
		}

		awsCfg, err := awsConfigForSnapshotTarget(ctx, cmd, cfg, target, credentialsFile, configPath, flagRole, payerSessions, configCache, &configMu)
		if err != nil {
			if target.IsLinked() {
				skipMu.Lock()
				skipped = append(skipped, snapshot.AccountWarning{
					AccountID:    accountID,
					DisplayAlias: target.DisplayAlias,
					Message:      snapshotAccountErrorMessage(err),
				})
				skipMu.Unlock()
				if bar != nil {
					bar.Advance()
				}
				return nil
			}
			return err
		}

		bt := snapshot.AccountTarget{
			AccountID:    accountID,
			DisplayAlias: target.DisplayAlias,
			AWSConfig:    awsCfg,
		}
		if err := enrichSnapshotTargetDisplayName(ctx, &bt, cfg, target); err != nil {
			return err
		}
		outMu.Lock()
		out = append(out, bt)
		outMu.Unlock()
		if bar != nil {
			bar.Advance()
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(skipped, func(i, j int) bool {
		return skipped[i].AccountID < skipped[j].AccountID
	})
	return out, skipped, nil
}

func snapshotAccountErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if idx := strings.Index(msg, ": "); idx >= 0 {
		prefix := strings.TrimSpace(msg[:idx])
		if len(prefix) == 12 {
			tail := strings.TrimSpace(msg[idx+2:])
			if tail != "" {
				return tail
			}
		}
	}
	if idx := strings.LastIndex(msg, ": "); idx >= 0 {
		tail := strings.TrimSpace(msg[idx+2:])
		if tail != "" {
			return tail
		}
	}
	return msg
}

func loadPayerProfileSessionsImpl(
	ctx context.Context,
	cfg configstore.File,
	targets []cost.AccountTarget,
	credentialsFile string,
) (map[string]awsconfig.ProfileSession, error) {
	seen := make(map[string]struct{})
	sessions := make(map[string]awsconfig.ProfileSession)
	for i := range targets {
		if !targets[i].IsLinked() {
			continue
		}
		payerID := targets[i].CredentialsAccountID()
		if _, ok := seen[payerID]; ok {
			continue
		}
		seen[payerID] = struct{}{}
		payerAlias := cfg.PayerAliasForAccountID(payerID)
		sess, err := awsconfig.ResolvePayerProfileSession(ctx, awsconfig.EnsureLinkedOptions{
			PayerAccountID:    payerID,
			PayerProfileNames: account.AWSProfileNames(payerID, payerAlias, nil),
			CredentialsPath:   credentialsFile,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", payerID, err)
		}
		sessions[payerID] = sess
	}
	return sessions, nil
}

func awsConfigForSnapshotTarget(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	target cost.AccountTarget,
	credentialsFile, configPath, flagRole string,
	payerSessions map[string]awsconfig.ProfileSession,
	configCache map[string]aws.Config,
	configMu *sync.Mutex,
) (aws.Config, error) {
	if target.IsLinked() {
		return linkedAWSConfigForSnapshotTarget(ctx, cmd, cfg, target, credentialsFile, configPath, flagRole, payerSessions, configCache, configMu)
	}

	credID := target.CredentialsAccountID()
	configMu.Lock()
	cached, ok := configCache[credID]
	configMu.Unlock()
	if ok {
		return cached, nil
	}
	awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, credID, credentialsFile)
	if err != nil {
		return aws.Config{}, err
	}
	configMu.Lock()
	configCache[credID] = awsCfg
	configMu.Unlock()
	return awsCfg, nil
}

func linkedAWSConfigForSnapshotTarget(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	target cost.AccountTarget,
	credentialsFile, configPath, flagRole string,
	payerSessions map[string]awsconfig.ProfileSession,
	configCache map[string]aws.Config,
	configMu *sync.Mutex,
) (aws.Config, error) {
	accountID := strings.TrimSpace(target.AccountID)
	payerID := target.CredentialsAccountID()

	configMu.Lock()
	cached, ok := configCache[accountID]
	configMu.Unlock()
	if ok {
		return cached, nil
	}

	roleARN, err := resolveSnapshotLinkedRoleARN(cmd, cfg, target, flagRole)
	if err != nil {
		return aws.Config{}, err
	}

	payerAlias := cfg.PayerAliasForAccountID(payerID)
	linkedSess, _, err := assumeSnapshotLinked(ctx, awsconfig.EnsureLinkedOptions{
		PayerAccountID:    payerID,
		LinkedAccountID:   accountID,
		RoleARN:           roleARN,
		CredentialsPath:   credentialsFile,
		PayerProfileNames: account.AWSProfileNames(payerID, payerAlias, nil),
		PayerSession:      payerSessions[payerID],
	})
	if err != nil {
		return aws.Config{}, fmt.Errorf("%s: %w", accountID, err)
	}

	awsCfg, err := awsconfig.LoadConfigFromSession(ctx, linkedSess)
	if err != nil {
		return aws.Config{}, fmt.Errorf("%s: load linked session: %w", accountID, err)
	}
	configMu.Lock()
	configCache[accountID] = awsCfg
	configMu.Unlock()
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
