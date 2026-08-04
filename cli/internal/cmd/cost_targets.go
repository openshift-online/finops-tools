// cost_targets.go resolves cost query account targets from explicit IDs/aliases, OUs, or org tags.
package cmd

import (
	"context"
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

type costTargetSelector struct {
	AccountIDs      []string
	Aliases         []string
	OUs             []configstore.OUSelector
	PayerAlias      string
	TagKey          string
	TagValue        string
	OrgCacheSkip    bool
	OrgCacheRefresh bool
	// SelectionRootID is set after OU/org resolution for --group-by ou rollup.
	SelectionRootID string
}

type costTargetSelectionMode int

const (
	costTargetModeNone costTargetSelectionMode = iota
	costTargetModeExplicit
	costTargetModeTag
	costTargetModeOU
	costTargetModeOrg
)

const errMixedAccountSelection = "provide only one account selection: --account-id/--account-alias, --ou, --tag, or --payer alone (not a combination)"

type costStepper interface {
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

var listOrganizationMemberAccounts = coreaccount.ListOrganizationMemberAccounts
var listAccountsUnderParent = coreaccount.ListAccountsUnderParent
var organizationRootID = coreaccount.OrganizationRootID
var buildOUAccountMapping = coreaccount.BuildOUAccountMapping

func parseCostTargetSelector(
	accountFlag, aliasFlag, ouFlag, payerFlag, tagFlag string,
	orgCacheSkip, orgCacheRefresh bool,
) (costTargetSelector, error) {
	sel := costTargetSelector{
		PayerAlias:      strings.TrimSpace(payerFlag),
		OrgCacheSkip:    orgCacheSkip,
		OrgCacheRefresh: orgCacheRefresh,
	}
	var err error

	if strings.TrimSpace(accountFlag) != "" {
		sel.AccountIDs, err = configstore.ParseAWSAccountIDs(accountFlag)
		if err != nil {
			return costTargetSelector{}, err
		}
	}
	if strings.TrimSpace(aliasFlag) != "" {
		sel.Aliases, err = configstore.ParseAccountAliases(aliasFlag)
		if err != nil {
			return costTargetSelector{}, err
		}
	}
	if strings.TrimSpace(ouFlag) != "" {
		sel.OUs, err = configstore.ParseOUSelectors(ouFlag)
		if err != nil {
			return costTargetSelector{}, err
		}
	}
	if strings.TrimSpace(tagFlag) != "" {
		sel.TagKey, sel.TagValue, err = parseTagFlag(tagFlag)
		if err != nil {
			return costTargetSelector{}, err
		}
	}
	return sel, nil
}

func parseTagFlag(s string) (key, value string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("--tag requires KEY or KEY=VALUE")
	}
	key, value, found := strings.Cut(s, "=")
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", fmt.Errorf("invalid --tag %q (expected KEY or KEY=VALUE)", s)
	}
	if found {
		value = strings.TrimSpace(value)
	}
	return key, value, nil
}

func validateOrgCacheFlags(skip, refresh bool) error {
	if skip && refresh {
		return fmt.Errorf("--skip-org-cache and --refresh-org-cache are mutually exclusive")
	}
	return nil
}

func awsReportSelectorSpecified(sel costTargetSelector) bool {
	return len(sel.AccountIDs) > 0 ||
		len(sel.Aliases) > 0 ||
		len(sel.OUs) > 0 ||
		sel.TagKey != "" ||
		sel.PayerAlias != ""
}

func validateReportCostTargetSelector(templateName string, sel costTargetSelector, snowflakeAlias string) error {
	switch reportpkg.AccountTargetModeFor(templateName) {
	case reportpkg.AccountTargetsSnowflake:
		if awsReportSelectorSpecified(sel) {
			return fmt.Errorf("%q report does not use AWS account targets (--account-id, --account-alias, --ou, --tag, --payer)", templateName)
		}
		if strings.TrimSpace(snowflakeAlias) == "" {
			return nil
		}
		return nil
	default:
		_, err := validateCostTargetSelector(sel)
		return err
	}
}

func validateCostTargetSelector(sel costTargetSelector) (costTargetSelectionMode, error) {
	explicit := len(sel.AccountIDs) > 0 || len(sel.Aliases) > 0
	ou := len(sel.OUs) > 0
	tag := sel.TagKey != ""
	payer := sel.PayerAlias != ""

	modeCount := 0
	if explicit {
		modeCount++
	}
	if ou {
		modeCount++
	}
	if tag {
		modeCount++
	}
	if payer && !explicit && !ou && !tag {
		modeCount++
	}

	if modeCount == 0 {
		return costTargetModeNone, fmt.Errorf("provide --account-id/--account-alias, --ou, --tag, or --payer alone")
	}
	if modeCount > 1 {
		return costTargetModeNone, fmt.Errorf("%s", errMixedAccountSelection)
	}

	if tag {
		if !payer {
			return costTargetModeNone, fmt.Errorf("--tag requires --payer")
		}
		return costTargetModeTag, nil
	}
	if ou {
		if !payer {
			return costTargetModeNone, fmt.Errorf("--ou requires --payer")
		}
		return costTargetModeOU, nil
	}
	if explicit {
		return costTargetModeExplicit, nil
	}
	if payer {
		return costTargetModeOrg, nil
	}
	return costTargetModeNone, fmt.Errorf("provide --account-id/--account-alias, --ou, --tag, or --payer alone")
}

func resolveCostTargets(
	cmd *cobra.Command,
	cfg configstore.File,
	sel *costTargetSelector,
	configPath, credentialsFile, authMethod string,
	status costStepper,
) ([]cost.AccountTarget, error) {
	mode, err := validateCostTargetSelector(*sel)
	if err != nil {
		return nil, err
	}

	ctx := awsCommandContext(cmd)
	switch mode {
	case costTargetModeTag:
		return resolveCostTargetsByTag(ctx, cmd, cfg, sel, configPath, credentialsFile, authMethod, status)
	case costTargetModeOU:
		return resolveCostTargetsWithOU(ctx, cmd, cfg, sel, configPath, credentialsFile, authMethod)
	case costTargetModeOrg:
		return resolveCostTargetsAllLinked(ctx, cmd, cfg, sel, configPath, credentialsFile, authMethod, status)
	case costTargetModeExplicit:
		return resolveCostTargetsExplicit(cfg, *sel)
	default:
		return nil, fmt.Errorf("invalid account selection")
	}
}

func resolveCostTargetsExplicit(cfg configstore.File, sel costTargetSelector) ([]cost.AccountTarget, error) {
	return configstore.ResolveCostTargets(cfg, sel.AccountIDs, sel.Aliases, sel.PayerAlias)
}

func resolveCostTargetsWithOU(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	sel *costTargetSelector,
	configPath, credentialsFile, authMethod string,
) ([]cost.AccountTarget, error) {
	payerID, ok := cfg.PayerAccountIDForAlias(sel.PayerAlias)
	if !ok {
		return nil, fmt.Errorf("unknown payer alias %q (register payer with: finops config account add aws <12-digit-id> --alias %s)", sel.PayerAlias, sel.PayerAlias)
	}
	payerTarget := cost.AccountTarget{AccountID: payerID}
	if err := ensureCostCredentials(ctx, cmd, cfg, []cost.AccountTarget{payerTarget}, configPath, credentialsFile, authMethod); err != nil {
		return nil, err
	}
	payerCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payerID, credentialsFile)
	if err != nil {
		return nil, err
	}

	memberIDs := make([]string, 0)
	seenMembers := make(map[string]struct{})
	namesByID := make(map[string]string)
	for _, ouSel := range sel.OUs {
		accounts, err := listAccountsUnderParent(ctx, payerCfg, ouSel.ID, coreaccount.ListAccountsInOUOptions{
			MaxDepth: ouSel.MaxDepth,
		})
		if err != nil {
			return nil, fmt.Errorf("OU %s: %w", ouSel.ID, err)
		}
		if len(accounts) == 0 {
			return nil, fmt.Errorf("no active accounts found in OU %s", ouSel.ID)
		}
		for _, acct := range accounts {
			if _, ok := seenMembers[acct.ID]; ok {
				continue
			}
			seenMembers[acct.ID] = struct{}{}
			memberIDs = append(memberIDs, acct.ID)
			namesByID[acct.ID] = acct.Name
		}
	}

	targets, err := configstore.ResolveOUAccountTargets(cfg, memberIDs, sel.PayerAlias)
	if err != nil {
		return nil, err
	}
	for i := range targets {
		if name, ok := namesByID[targets[i].AccountID]; ok {
			targets[i].DisplayName = name
		}
	}
	if len(sel.OUs) == 1 {
		sel.SelectionRootID = sel.OUs[0].ID
	}
	return targets, nil
}

func resolveCostTargetsAllLinked(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	sel *costTargetSelector,
	configPath, credentialsFile, authMethod string,
	status costStepper,
) ([]cost.AccountTarget, error) {
	payerAlias := sel.PayerAlias
	payerID, ok := cfg.PayerAccountIDForAlias(payerAlias)
	if !ok {
		return nil, fmt.Errorf("unknown payer alias %q (register payer with: finops config account add aws <12-digit-id> --alias %s)", payerAlias, payerAlias)
	}

	costStep(status, "Ensuring AWS credentials for payer…")
	payerTarget := cost.AccountTarget{AccountID: payerID}
	if err := ensureCostCredentials(ctx, cmd, cfg, []cost.AccountTarget{payerTarget}, configPath, credentialsFile, authMethod); err != nil {
		return nil, err
	}

	awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payerID, credentialsFile)
	if err != nil {
		return nil, err
	}

	rootID, err := organizationRootID(ctx, awsCfg)
	if err != nil {
		return nil, fmt.Errorf("resolve organization root: %w", err)
	}
	sel.SelectionRootID = rootID

	costStep(status, "Resolving organization member accounts…")
	members, err := listOrganizationMemberAccounts(ctx, awsCfg, payerID)
	if err != nil {
		return nil, fmt.Errorf("list organization member accounts: %w", err)
	}

	memberIDs := make([]string, 0, len(members))
	for _, acct := range members {
		if acct.ID == payerID {
			continue
		}
		memberIDs = append(memberIDs, acct.ID)
	}

	targets, err := configstore.ResolveOUAccountTargets(cfg, memberIDs, payerAlias)
	if err != nil {
		return nil, err
	}
	namesByID := make(map[string]string, len(members))
	for _, acct := range members {
		namesByID[acct.ID] = acct.Name
	}
	for i := range targets {
		if name, ok := namesByID[targets[i].AccountID]; ok {
			targets[i].DisplayName = name
		}
	}
	return targets, nil
}

func resolveCostTargetsByTag(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	sel *costTargetSelector,
	configPath, credentialsFile, authMethod string,
	status costStepper,
) ([]cost.AccountTarget, error) {
	payerAlias := sel.PayerAlias
	payerID, ok := cfg.PayerAccountIDForAlias(payerAlias)
	if !ok {
		return nil, fmt.Errorf("unknown payer alias %q (register payer with: finops config account add aws <12-digit-id> --alias %s)", payerAlias, payerAlias)
	}

	tagKey := sel.TagKey
	tagValue := sel.TagValue

	costStep(status, "Ensuring AWS credentials for payer…")
	payerTarget := cost.AccountTarget{AccountID: payerID}
	if err := ensureCostCredentials(ctx, cmd, cfg, []cost.AccountTarget{payerTarget}, configPath, credentialsFile, authMethod); err != nil {
		return nil, err
	}

	awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payerID, credentialsFile)
	if err != nil {
		return nil, err
	}

	if tagValue != "" {
		costStep(status, fmt.Sprintf("Resolving accounts with tag %s=%q…", tagKey, tagValue))
	} else {
		costStep(status, fmt.Sprintf("Resolving accounts with tag key %q…", tagKey))
	}
	matches, err := filterOrganizationAccountsByTag(ctx, awsCfg, payerID, tagKey, tagValue, status, configPath, sel.OrgCacheSkip, sel.OrgCacheRefresh)
	if err != nil {
		return nil, fmt.Errorf("list accounts by tag: %w", err)
	}
	if len(matches) == 0 {
		if tagValue != "" {
			costStep(status, fmt.Sprintf("No accounts matched tag %s=%q", tagKey, tagValue))
		} else {
			costStep(status, fmt.Sprintf("No accounts matched tag key %q", tagKey))
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

	rootID, err := organizationRootID(ctx, awsCfg)
	if err != nil {
		return nil, fmt.Errorf("resolve organization root: %w", err)
	}
	sel.SelectionRootID = rootID
	return targets, nil
}

func resolveAccountOUBuckets(
	ctx context.Context,
	cfg configstore.File,
	sel costTargetSelector,
	targets []cost.AccountTarget,
	credentialsFile string,
) (map[string]cost.OUBucket, []cost.OUHierarchyNode, string, error) {
	if len(targets) == 0 {
		return nil, nil, "", nil
	}

	groups := cost.GroupByCredentialsAccount(targets)
	useSelectionRoot := len(groups) == 1 && strings.TrimSpace(sel.SelectionRootID) != ""

	out := make(map[string]cost.OUBucket)
	var nodes []cost.OUHierarchyNode
	var primaryRoot string

	for _, payerID := range cost.SortedCredentialAccountIDs(groups) {
		group := groups[payerID]
		awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payerID, credentialsFile)
		if err != nil {
			return nil, nil, "", fmt.Errorf("load credentials for payer %s: %w", payerID, err)
		}

		rootID := ""
		if useSelectionRoot {
			rootID = strings.TrimSpace(sel.SelectionRootID)
		}
		if rootID == "" {
			rootID, err = organizationRootID(ctx, awsCfg)
			if err != nil {
				return nil, nil, "", fmt.Errorf("resolve organization root for payer %s: %w", payerID, err)
			}
		}
		if primaryRoot == "" {
			primaryRoot = rootID
		}

		ids := make([]string, 0, len(group))
		for _, t := range group {
			ids = append(ids, t.AccountID)
		}
		mapped, hierarchy, err := buildOUAccountMapping(ctx, awsCfg, rootID, ids)
		if err != nil {
			return nil, nil, "", fmt.Errorf("map accounts to OUs for payer %s: %w", payerID, err)
		}
		for accountID, bucket := range mapped {
			out[accountID] = cost.OUBucket{ID: bucket.ID, Name: bucket.Name}
		}
		for _, n := range hierarchy {
			nodes = append(nodes, cost.OUHierarchyNode{
				ID:       n.ID,
				Name:     n.Name,
				ParentID: n.ParentID,
				Depth:    n.Depth,
			})
		}
	}
	return out, nodes, primaryRoot, nil
}

func costStep(status costStepper, message string) {
	if status == nil {
		return
	}
	status.Step(message)
}

func mergeCostTargets(segments ...[]cost.AccountTarget) []cost.AccountTarget {
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