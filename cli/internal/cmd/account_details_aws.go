// account_details_aws.go ensures AWS credentials for account review inventory targets.
package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/account"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/inventory"
	"github.com/openshift-online/finops-tools/core/parallel"
	"github.com/spf13/cobra"
)

var (
	ensureAccountDetailsCredentials   = ensureAccountDetailsCredentialsImpl
	prepareAccountDetailsTargets      = prepareAccountDetailsTargetsImpl
	assumeAccountDetailsLinked        = awsconfig.AssumeLinkedCredentials
	resolveAccountDetailsPayerSession = awsconfig.ResolvePayerProfileSession
)

func ensureAccountDetailsCredentialsImpl(
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

// prepareAccountDetailsTargetsImpl builds inventory targets with payer or
// assumed-role credentials. Linked assume-role is deferred to inventory.Scan
// (via ConfigLoader) so a failed member session is recorded as InventoryError
// on that account instead of skipping email.
func prepareAccountDetailsTargetsImpl(
	cmd *cobra.Command,
	cfg configstore.File,
	targets []cost.AccountTarget,
	credentialsFile, configPath, flagRole string,
	workers int,
	bar *progress.Bar,
) ([]inventory.AccountTarget, error) {
	ctx := awsCommandContext(cmd)
	if bar != nil {
		defer bar.Finish()
	}
	var (
		configMu sync.Mutex
		outMu    sync.Mutex
	)
	configCache := make(map[string]aws.Config)
	var out []inventory.AccountTarget

	err := parallel.ForEach(ctx, workers, len(targets), func(ctx context.Context, i int) error {
		target := targets[i]
		accountID := strings.TrimSpace(target.AccountID)
		if accountID == "" {
			return fmt.Errorf("account target %d: account ID is required", i+1)
		}

		var invTarget inventory.AccountTarget
		if target.IsLinked() {
			loader, loaderErr := linkedAccountDetailsConfigLoader(cmd, cfg, target, credentialsFile, configPath, flagRole)
			if loaderErr != nil {
				return loaderErr
			}
			invTarget = inventory.AccountTarget{
				AccountID:    accountID,
				DisplayAlias: target.DisplayAlias,
				ConfigLoader: cachedConfigLoader(loader),
			}
		} else {
			payerCfg, loadErr := awsConfigForAccountDetailsTarget(ctx, cfg, target, credentialsFile, configCache, &configMu)
			if loadErr != nil {
				return loadErr
			}
			invTarget = inventory.AccountTarget{
				AccountID:    accountID,
				DisplayAlias: target.DisplayAlias,
				AWSConfig:    payerCfg,
			}
		}

		if err := enrichAccountDetailsDisplayName(ctx, &invTarget, cfg, target); err != nil {
			return err
		}
		outMu.Lock()
		out = append(out, invTarget)
		outMu.Unlock()
		if bar != nil {
			bar.Advance()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func awsConfigForAccountDetailsTarget(
	ctx context.Context,
	cfg configstore.File,
	target cost.AccountTarget,
	credentialsFile string,
	configCache map[string]aws.Config,
	configMu *sync.Mutex,
) (aws.Config, error) {
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

// cachedConfigLoader runs load at most once so prepare and inventory.Scan share
// a single assume-role if both need the session.
func cachedConfigLoader(load func(context.Context) (aws.Config, error)) func(context.Context) (aws.Config, error) {
	var (
		mu   sync.Mutex
		cfg  aws.Config
		err  error
		done bool
	)
	return func(ctx context.Context) (aws.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return cfg, err
		}
		cfg, err = load(ctx)
		done = true
		return cfg, err
	}
}

// linkedAccountDetailsConfigLoader returns a lazy assume-role loader so
// inventory.Scan performs the Organizations-linked session when it runs.
func linkedAccountDetailsConfigLoader(
	cmd *cobra.Command,
	cfg configstore.File,
	target cost.AccountTarget,
	credentialsFile, configPath, flagRole string,
) (func(context.Context) (aws.Config, error), error) {
	accountID := strings.TrimSpace(target.AccountID)
	payerID := target.CredentialsAccountID()
	roleARN, err := resolveTargetLinkedRoleARN(cmd, cfg, configPath, accountID, target.DisplayAlias, flagRole)
	if err != nil {
		return nil, err
	}
	payerAlias := cfg.PayerAliasForAccountID(payerID)
	payerProfiles := account.AWSProfileNames(payerID, payerAlias, nil)

	loader := func(ctx context.Context) (aws.Config, error) {
		payerSess, err := resolveAccountDetailsPayerSession(ctx, awsconfig.EnsureLinkedOptions{
			PayerAccountID:    payerID,
			PayerProfileNames: payerProfiles,
			CredentialsPath:   credentialsFile,
		})
		if err != nil {
			return aws.Config{}, fmt.Errorf("%s: %w", payerID, err)
		}
		linkedSess, _, err := assumeAccountDetailsLinked(ctx, awsconfig.EnsureLinkedOptions{
			PayerAccountID:    payerID,
			LinkedAccountID:   accountID,
			RoleARN:           roleARN,
			CredentialsPath:   credentialsFile,
			PayerProfileNames: payerProfiles,
			PayerSession:      payerSess,
		})
		if err != nil {
			return aws.Config{}, fmt.Errorf("%s: %w", accountID, err)
		}
		awsCfg, err := awsconfig.LoadConfigFromSession(ctx, linkedSess)
		if err != nil {
			return aws.Config{}, fmt.Errorf("%s: load linked session: %w", accountID, err)
		}
		return awsCfg, nil
	}
	return loader, nil
}

func enrichAccountDetailsDisplayName(
	ctx context.Context,
	target *inventory.AccountTarget,
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
	if target.DisplayName == "" && target.ConfigLoader == nil {
		name, err := coreaccount.AccountName(ctx, target.AWSConfig, target.AccountID)
		if err == nil {
			target.DisplayName = name
		}
	}
	return nil
}
