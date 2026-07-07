// account_target_resolve.go resolves account targets from explicit IDs/aliases, OUs, or org tags.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/orgcache"
	reportpkg "github.com/openshift-online/finops-tools/cli/internal/report"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

type accountTargetSelector struct {
	AccountIDs      []string
	Aliases         []string
	OUIDs           []string
	PayerAlias      string
	OUDirectOnly    bool
	TagKey          string
	TagValue        string
	OrgCacheSkip    bool
	OrgCacheRefresh bool
}

type accountTargetSelectionMode int

const (
	accountTargetModeNone accountTargetSelectionMode = iota
	accountTargetModeExplicit
	accountTargetModeTag
	accountTargetModeOU
)

type stepper interface {
	Step(string)
}

var filterOrganizationAccountsByTag = func(
	ctx context.Context,
	awsCfg aws.Config,
	payerID, tagKey, tagValue string,
	progress coreaccount.TagFilterProgress,
	configPath string,
	orgCacheSkip, orgCacheRefresh bool,
) ([]coreaccount.OrganizationAccount, error) {
	return orgcache.FilterOrganizationAccountsByTag(ctx, awsCfg, tagKey, tagValue, progress, orgcache.Options{
		ConfigPath: configPath,
		PayerID:    payerID,
		Skip:       orgCacheSkip,
		Refresh:    orgCacheRefresh,
	})
}

func parseAccountTargetSelector(
	accountFlag, aliasFlag, ouFlag, payerFlag, tagKey, tagValue string,
	ouDirect, orgCacheSkip, orgCacheRefresh bool,
) (accountTargetSelector, error) {
	sel := accountTargetSelector{
		PayerAlias:      strings.TrimSpace(payerFlag),
		OUDirectOnly:    ouDirect,
		TagKey:          strings.TrimSpace(tagKey),
		TagValue:        strings.TrimSpace(tagValue),
		OrgCacheSkip:    orgCacheSkip,
		OrgCacheRefresh: orgCacheRefresh,
	}
	var err error

	if strings.TrimSpace(accountFlag) != "" {
		sel.AccountIDs, err = configstore.ParseAWSAccountIDs(accountFlag)
		if err != nil {
			return accountTargetSelector{}, err
		}
	}
	if strings.TrimSpace(aliasFlag) != "" {
		sel.Aliases, err = configstore.ParseAccountAliases(aliasFlag)
		if err != nil {
			return accountTargetSelector{}, err
		}
	}
	if strings.TrimSpace(ouFlag) != "" {
		sel.OUIDs, err = configstore.ParseOUIDs(ouFlag)
		if err != nil {
			return accountTargetSelector{}, err
		}
	}
	return sel, nil
}

func validateOrgCacheFlags(skip, refresh bool) error {
	if skip && refresh {
		return fmt.Errorf("--skip-org-cache and --refresh-org-cache are mutually exclusive")
	}
	return nil
}

func accountTargetSelectorSpecified(sel accountTargetSelector) bool {
	return len(sel.AccountIDs) > 0 ||
		len(sel.Aliases) > 0 ||
		len(sel.OUIDs) > 0 ||
		sel.TagKey != "" ||
		sel.PayerAlias != "" ||
		sel.OUDirectOnly
}

func reportAccountTargetSpecified(sel accountTargetSelector) bool {
	return len(sel.AccountIDs) > 0 ||
		len(sel.OUIDs) > 0 ||
		sel.TagKey != "" ||
		sel.PayerAlias != "" ||
		sel.OUDirectOnly
}

func validateReportAccountTargetSelector(templateName string, sel accountTargetSelector, snowflakeAlias string) error {
	switch reportpkg.AccountTargetModeFor(templateName) {
	case reportpkg.AccountTargetsSnowflake:
		if reportAccountTargetSpecified(sel) || len(sel.Aliases) > 0 {
			return fmt.Errorf("%q report does not use AWS account targets (--account, --account-alias, --ou, --tag-key, --payer)", templateName)
		}
		if strings.TrimSpace(snowflakeAlias) == "" {
			return nil
		}
		return nil
	case reportpkg.AccountTargetsOptional:
		if !accountTargetSelectorSpecified(sel) {
			return nil
		}
		_, err := validateAccountTargetSelector(sel)
		return err
	default:
		_, err := validateAccountTargetSelector(sel)
		return err
	}
}

func validateAccountTargetSelector(sel accountTargetSelector) (accountTargetSelectionMode, error) {
	tag := sel.TagKey != ""
	explicit := len(sel.AccountIDs) > 0 || len(sel.Aliases) > 0
	ou := len(sel.OUIDs) > 0

	if tag {
		if explicit || ou {
			return accountTargetModeNone, fmt.Errorf("provide either --account/--account-alias/--ou or --tag-key, not both")
		}
		if sel.PayerAlias == "" {
			return accountTargetModeNone, fmt.Errorf("--payer is required with --tag-key")
		}
		return accountTargetModeTag, nil
	}

	if sel.PayerAlias != "" && !explicit && !ou {
		return accountTargetModeNone, fmt.Errorf("--payer requires --account or --ou")
	}
	if !explicit && !ou {
		return accountTargetModeNone, fmt.Errorf("provide --account/--account-alias, --ou, or --tag-key")
	}
	if ou && sel.PayerAlias == "" {
		return accountTargetModeNone, fmt.Errorf("--ou requires --payer")
	}
	if sel.OUDirectOnly && !ou {
		return accountTargetModeNone, fmt.Errorf("--ou-direct requires --ou")
	}
	if sel.PayerAlias != "" && explicit && len(sel.AccountIDs) == 0 {
		return accountTargetModeNone, fmt.Errorf("--payer requires --account")
	}

	if ou {
		return accountTargetModeOU, nil
	}
	return accountTargetModeExplicit, nil
}

func resolveAccountTargets(
	cmd *cobra.Command,
	cfg configstore.File,
	sel accountTargetSelector,
	configPath, credentialsFile, authMethod string,
	status stepper,
) ([]cost.AccountTarget, error) {
	mode, err := validateAccountTargetSelector(sel)
	if err != nil {
		return nil, err
	}

	ctx := awsCommandContext(cmd)
	switch mode {
	case accountTargetModeTag:
		return resolveAccountTargetsByTag(ctx, cmd, cfg, sel, configPath, credentialsFile, authMethod, status)
	case accountTargetModeOU:
		return resolveAccountTargetsWithOU(ctx, cmd, cfg, sel, configPath, credentialsFile, authMethod)
	case accountTargetModeExplicit:
		return resolveAccountTargetsExplicit(cfg, sel)
	default:
		return nil, fmt.Errorf("invalid account selection")
	}
}

func resolveAccountTargetsExplicit(cfg configstore.File, sel accountTargetSelector) ([]cost.AccountTarget, error) {
	return configstore.ResolveCostTargets(cfg, sel.AccountIDs, sel.Aliases, sel.PayerAlias)
}

func resolveAccountTargetsWithOU(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	sel accountTargetSelector,
	configPath, credentialsFile, authMethod string,
) ([]cost.AccountTarget, error) {
	var ouTargets, explicitTargets []cost.AccountTarget
	var err error

	if len(sel.OUIDs) > 0 {
		payerID, ok := cfg.PayerAccountIDForAlias(sel.PayerAlias)
		if !ok {
			return nil, fmt.Errorf("unknown payer alias %q (register payer with: finops config account add aws <12-digit-id> --alias %s)", sel.PayerAlias, sel.PayerAlias)
		}
		payerTarget := cost.AccountTarget{AccountID: payerID}
		if err := ensureAccountCredentials(ctx, cmd, cfg, []cost.AccountTarget{payerTarget}, configPath, credentialsFile, authMethod); err != nil {
			return nil, err
		}
		payerCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payerID, credentialsFile)
		if err != nil {
			return nil, err
		}

		memberIDs := make([]string, 0)
		seenMembers := make(map[string]struct{})
		for _, ouID := range sel.OUIDs {
			accounts, err := coreaccount.ListAccountsInOU(ctx, payerCfg, ouID, coreaccount.ListAccountsInOUOptions{
				DirectOnly: sel.OUDirectOnly,
			})
			if err != nil {
				return nil, fmt.Errorf("OU %s: %w", ouID, err)
			}
			if len(accounts) == 0 {
				return nil, fmt.Errorf("no active accounts found in OU %s", ouID)
			}
			for _, acct := range accounts {
				if _, ok := seenMembers[acct.ID]; ok {
					continue
				}
				seenMembers[acct.ID] = struct{}{}
				memberIDs = append(memberIDs, acct.ID)
			}
		}

		ouTargets, err = configstore.ResolveOUAccountTargets(cfg, memberIDs, sel.PayerAlias)
		if err != nil {
			return nil, err
		}
	}

	if len(sel.AccountIDs) > 0 || len(sel.Aliases) > 0 {
		explicitTargets, err = resolveAccountTargetsExplicit(cfg, sel)
		if err != nil {
			return nil, err
		}
	}

	targets := mergeAccountTargets(ouTargets, explicitTargets)
	if len(targets) == 0 {
		return nil, errors.New("no accounts selected")
	}
	return targets, nil
}

func mergeAccountTargets(segments ...[]cost.AccountTarget) []cost.AccountTarget {
	seen := make(map[string]cost.AccountTarget)
	order := make([]string, 0)
	for _, segment := range segments {
		for _, target := range segment {
			id := strings.TrimSpace(target.AccountID)
			if id == "" {
				continue
			}
			if existing, ok := seen[id]; ok {
				if existing.DisplayAlias == "" && target.DisplayAlias != "" {
					seen[id] = target
				}
				continue
			}
			seen[id] = target
			order = append(order, id)
		}
	}
	out := make([]cost.AccountTarget, 0, len(order))
	for _, id := range order {
		out = append(out, seen[id])
	}
	return out
}

func resolveAccountTargetsByTag(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	sel accountTargetSelector,
	configPath, credentialsFile, authMethod string,
	status stepper,
) ([]cost.AccountTarget, error) {
	payerAlias := sel.PayerAlias
	payerID, ok := cfg.PayerAccountIDForAlias(payerAlias)
	if !ok {
		return nil, fmt.Errorf("unknown payer alias %q (register payer with: finops config account add aws <12-digit-id> --alias %s)", payerAlias, payerAlias)
	}

	tagKey := sel.TagKey
	tagValue := sel.TagValue

	progressStep(status, "Ensuring AWS credentials for payer…")
	payerTarget := cost.AccountTarget{AccountID: payerID}
	if err := ensureAccountCredentials(ctx, cmd, cfg, []cost.AccountTarget{payerTarget}, configPath, credentialsFile, authMethod); err != nil {
		return nil, err
	}

	awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payerID, credentialsFile)
	if err != nil {
		return nil, err
	}

	if tagValue != "" {
		progressStep(status, fmt.Sprintf("Resolving accounts with tag %s=%q…", tagKey, tagValue))
	} else {
		progressStep(status, fmt.Sprintf("Resolving accounts with tag key %q…", tagKey))
	}
	matches, err := filterOrganizationAccountsByTag(ctx, awsCfg, payerID, tagKey, tagValue, status, configPath, sel.OrgCacheSkip, sel.OrgCacheRefresh)
	if err != nil {
		return nil, fmt.Errorf("list accounts by tag: %w", err)
	}
	if len(matches) == 0 {
		if tagValue != "" {
			progressStep(status, fmt.Sprintf("No accounts matched tag %s=%q", tagKey, tagValue))
		} else {
			progressStep(status, fmt.Sprintf("No accounts matched tag key %q", tagKey))
		}
		return nil, nil
	}

	targets := make([]cost.AccountTarget, 0, len(matches))
	for _, acct := range matches {
		displayAlias := cfg.AliasForAccountID(acct.ID)
		if displayAlias == acct.ID {
			displayAlias = ""
		}
		targets = append(targets, cost.AccountTarget{
			AccountID:        acct.ID,
			PayerAccountID:   payerID,
			ScopeAccountOnly: true,
			DisplayAlias:     displayAlias,
			DisplayName:      acct.Name,
		})
	}
	return targets, nil
}

func progressStep(status stepper, message string) {
	if status == nil {
		return
	}
	status.Step(message)
}

func shouldReportIndexedProgress(index, total int) bool {
	if total <= 1 {
		return false
	}
	if index == 1 || index == total {
		return true
	}
	if total <= 10 {
		return true
	}
	return index%25 == 0
}
