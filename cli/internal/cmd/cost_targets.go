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
	AccountIDs            []string
	Aliases               []string
	OUs                   []configstore.OUSelector
	PayerAliases          []string
	TagKey                string
	TagValue              string
	OrgCacheSkip          bool
	OrgCacheRefresh       bool
	IncludeClosedAccounts bool
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
const errMultiplePayersExplicit = "--payer accepts multiple aliases only for org, OU, or tag selection"

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
	orgCacheSkip, orgCacheRefresh, includeClosedAccounts bool,
) (costTargetSelector, error) {
	sel := costTargetSelector{
		OrgCacheSkip:          orgCacheSkip,
		OrgCacheRefresh:       orgCacheRefresh,
		IncludeClosedAccounts: includeClosedAccounts,
	}
	var err error

	if strings.TrimSpace(payerFlag) != "" {
		sel.PayerAliases, err = configstore.ParsePayerAliases(payerFlag)
		if err != nil {
			return costTargetSelector{}, err
		}
	}
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
		len(sel.PayerAliases) > 0
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
	payer := len(sel.PayerAliases) > 0

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
		if len(sel.PayerAliases) > 1 {
			return costTargetModeNone, fmt.Errorf("%s", errMultiplePayersExplicit)
		}
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

	warnIfIncludeClosedAccountsIgnored(cmd, *sel, mode)

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

func warnIfIncludeClosedAccountsIgnored(cmd *cobra.Command, sel costTargetSelector, mode costTargetSelectionMode) {
	if !sel.IncludeClosedAccounts {
		return
	}
	var msg string
	switch mode {
	case costTargetModeExplicit:
		msg = "warning: --include-closed-accounts has no effect with --account-id/--account-alias (accounts are selected explicitly regardless of Organizations state)\n"
	case costTargetModeTag:
		msg = "warning: --include-closed-accounts has no effect with --tag (tag selection already includes all Organizations states)\n"
	default:
		return
	}
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), msg)
}

func resolveCostTargetsExplicit(cfg configstore.File, sel costTargetSelector) ([]cost.AccountTarget, error) {
	var payerAlias string
	if len(sel.PayerAliases) == 1 {
		payerAlias = sel.PayerAliases[0]
	}
	return configstore.ResolveCostTargets(cfg, sel.AccountIDs, sel.Aliases, payerAlias)
}

func organizationMemberListOptions(includeClosed bool) coreaccount.ListAccountsInOUOptions {
	return coreaccount.ListAccountsInOUOptions{IncludeClosed: includeClosed}
}

type resolvedPayer struct {
	Alias     string
	AccountID string
}

func resolveSelectorPayers(cfg configstore.File, aliases []string) ([]resolvedPayer, error) {
	if len(aliases) == 0 {
		return nil, fmt.Errorf("payer alias is required")
	}
	seen := make(map[string]struct{}, len(aliases))
	out := make([]resolvedPayer, 0, len(aliases))
	for _, alias := range aliases {
		id, ok := cfg.PayerAccountIDForAlias(alias)
		if !ok {
			return nil, fmt.Errorf("unknown payer alias %q (register payer with: finops config account add aws <12-digit-id> --alias %s)", alias, alias)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, resolvedPayer{Alias: alias, AccountID: id})
	}
	return out, nil
}

func payerAliasList(payers []resolvedPayer) string {
	aliases := make([]string, len(payers))
	for i, payer := range payers {
		aliases[i] = payer.Alias
	}
	return strings.Join(aliases, ", ")
}

func payerNameByAccountID(payers []resolvedPayer) map[string]string {
	names := make(map[string]string, len(payers))
	for _, payer := range payers {
		names[payer.AccountID] = payer.Alias
	}
	return names
}

func ensurePayerCredentials(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	payers []resolvedPayer,
	configPath, credentialsFile, authMethod string,
	status costStepper,
) error {
	if len(payers) == 1 {
		costStep(status, "Ensuring AWS credentials for payer…")
	} else if len(payers) > 1 {
		costStep(status, "Ensuring AWS credentials for payers…")
	}
	targets := make([]cost.AccountTarget, len(payers))
	for i, payer := range payers {
		targets[i] = cost.AccountTarget{AccountID: payer.AccountID}
	}
	return ensureCostCredentials(ctx, cmd, cfg, targets, configPath, credentialsFile, authMethod)
}

func appendCostTargets(dst []cost.AccountTarget, seen map[string]cost.AccountTarget, add []cost.AccountTarget, payerNames map[string]string) ([]cost.AccountTarget, error) {
	for _, target := range add {
		id := strings.TrimSpace(target.AccountID)
		if id == "" {
			continue
		}
		if prev, ok := seen[id]; ok {
			if prev.CredentialsAccountID() != target.CredentialsAccountID() {
				return nil, fmt.Errorf("account %s resolved under payers %s and %s", id, payerLabel(prev, payerNames), payerLabel(target, payerNames))
			}
			continue
		}
		seen[id] = target
		dst = append(dst, target)
	}
	return dst, nil
}

func payerLabel(target cost.AccountTarget, payerNames map[string]string) string {
	id := target.CredentialsAccountID()
	if alias := payerNames[id]; alias != "" {
		return alias
	}
	return id
}

func applyOrganizationNames(targets []cost.AccountTarget, namesByID map[string]string) {
	for i := range targets {
		if name, ok := namesByID[targets[i].AccountID]; ok {
			targets[i].DisplayName = name
		}
	}
}

func resolveCostTargetsWithOU(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	sel *costTargetSelector,
	configPath, credentialsFile, authMethod string,
) ([]cost.AccountTarget, error) {
	payers, err := resolveSelectorPayers(cfg, sel.PayerAliases)
	if err != nil {
		return nil, err
	}
	if err := ensurePayerCredentials(ctx, cmd, cfg, payers, configPath, credentialsFile, authMethod, nil); err != nil {
		return nil, err
	}

	type memberHit struct {
		account coreaccount.OrganizationAccount
		payer   resolvedPayer
	}
	seenPayerByAccount := make(map[string]string)
	hits := make([]memberHit, 0)
	listOpts := organizationMemberListOptions(sel.IncludeClosedAccounts)
	for _, ouSel := range sel.OUs {
		found := false
		returned := 0
		for _, payer := range payers {
			payerCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payer.AccountID, credentialsFile)
			if err != nil {
				return nil, err
			}
			accounts, err := listAccountsUnderParent(ctx, payerCfg, ouSel.ID, coreaccount.ListAccountsInOUOptions{
				MaxDepth:      ouSel.MaxDepth,
				IncludeClosed: listOpts.IncludeClosed,
			})
			if err != nil {
				if len(payers) > 1 && coreaccount.IsParentNotFound(err) {
					continue
				}
				return nil, fmt.Errorf("OU %s under payer %s: %w", ouSel.ID, payerErrorLabel(payer), err)
			}
			found = true
			returned += len(accounts)
			for _, acct := range accounts {
				if prev, ok := seenPayerByAccount[acct.ID]; ok {
					if prev != payer.AccountID {
						return nil, fmt.Errorf("account %s resolved under payers %s and %s", acct.ID, payerNamesLabel(prev, payers), payer.Alias)
					}
					continue
				}
				seenPayerByAccount[acct.ID] = payer.AccountID
				hits = append(hits, memberHit{account: acct, payer: payer})
			}
		}
		if !found {
			return nil, fmt.Errorf("OU %s was not found under payer(s) %s", ouSel.ID, payerAliasList(payers))
		}
		if returned == 0 {
			if sel.IncludeClosedAccounts {
				return nil, fmt.Errorf("no accounts found in OU %s", ouSel.ID)
			}
			return nil, fmt.Errorf("no active accounts found in OU %s", ouSel.ID)
		}
	}

	namesByID := make(map[string]string, len(hits))
	idsByPayer := make(map[string][]string)
	var payerOrder []resolvedPayer
	payerSeen := make(map[string]struct{})
	for _, hit := range hits {
		namesByID[hit.account.ID] = hit.account.Name
		if _, ok := payerSeen[hit.payer.AccountID]; !ok {
			payerSeen[hit.payer.AccountID] = struct{}{}
			payerOrder = append(payerOrder, hit.payer)
		}
		idsByPayer[hit.payer.AccountID] = append(idsByPayer[hit.payer.AccountID], hit.account.ID)
	}

	var targets []cost.AccountTarget
	for _, payer := range payerOrder {
		part, err := configstore.ResolveOUAccountTargets(cfg, idsByPayer[payer.AccountID], payer.Alias)
		if err != nil {
			return nil, err
		}
		applyOrganizationNames(part, namesByID)
		targets = append(targets, part...)
	}
	if len(sel.OUs) == 1 {
		sel.SelectionRootID = sel.OUs[0].ID
	}
	return targets, nil
}

func payerErrorLabel(payer resolvedPayer) string {
	return fmt.Sprintf("%s (%s)", payer.Alias, payer.AccountID)
}

func payerNamesLabel(accountID string, payers []resolvedPayer) string {
	for _, payer := range payers {
		if payer.AccountID == accountID {
			return payer.Alias
		}
	}
	return accountID
}

func resolveCostTargetsAllLinked(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	sel *costTargetSelector,
	configPath, credentialsFile, authMethod string,
	status costStepper,
) ([]cost.AccountTarget, error) {
	payers, err := resolveSelectorPayers(cfg, sel.PayerAliases)
	if err != nil {
		return nil, err
	}
	if err := ensurePayerCredentials(ctx, cmd, cfg, payers, configPath, credentialsFile, authMethod, status); err != nil {
		return nil, err
	}

	payerNames := payerNameByAccountID(payers)
	seen := make(map[string]cost.AccountTarget)
	var targets []cost.AccountTarget
	for _, payer := range payers {
		if len(payers) == 1 {
			costStep(status, "Resolving organization member accounts…")
		} else {
			costStep(status, fmt.Sprintf("Resolving organization member accounts for %s…", payer.Alias))
		}
		part, rootID, err := resolveOneOrganization(ctx, cfg, sel, payer, credentialsFile)
		if err != nil {
			return nil, err
		}
		targets, err = appendCostTargets(targets, seen, part, payerNames)
		if err != nil {
			return nil, err
		}
		if len(payers) == 1 {
			sel.SelectionRootID = rootID
		}
	}
	return targets, nil
}

func resolveOneOrganization(
	ctx context.Context,
	cfg configstore.File,
	sel *costTargetSelector,
	payer resolvedPayer,
	credentialsFile string,
) ([]cost.AccountTarget, string, error) {
	awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payer.AccountID, credentialsFile)
	if err != nil {
		return nil, "", err
	}
	rootID, err := organizationRootID(ctx, awsCfg)
	if err != nil {
		return nil, "", fmt.Errorf("resolve organization root for payer %s: %w", payerErrorLabel(payer), err)
	}
	members, err := listOrganizationMemberAccounts(ctx, awsCfg, payer.AccountID, organizationMemberListOptions(sel.IncludeClosedAccounts))
	if err != nil {
		return nil, "", fmt.Errorf("list organization member accounts for payer %s: %w", payerErrorLabel(payer), err)
	}
	memberIDs := make([]string, 0, len(members))
	namesByID := make(map[string]string, len(members))
	for _, acct := range members {
		namesByID[acct.ID] = acct.Name
		if acct.ID == payer.AccountID {
			continue
		}
		memberIDs = append(memberIDs, acct.ID)
	}
	targets, err := configstore.ResolveOUAccountTargets(cfg, memberIDs, payer.Alias)
	if err != nil {
		return nil, "", err
	}
	applyOrganizationNames(targets, namesByID)
	return targets, rootID, nil
}

func resolveCostTargetsByTag(
	ctx context.Context,
	cmd *cobra.Command,
	cfg configstore.File,
	sel *costTargetSelector,
	configPath, credentialsFile, authMethod string,
	status costStepper,
) ([]cost.AccountTarget, error) {
	payers, err := resolveSelectorPayers(cfg, sel.PayerAliases)
	if err != nil {
		return nil, err
	}
	if err := ensurePayerCredentials(ctx, cmd, cfg, payers, configPath, credentialsFile, authMethod, status); err != nil {
		return nil, err
	}

	tagKey := sel.TagKey
	tagValue := sel.TagValue
	if tagValue != "" {
		costStep(status, fmt.Sprintf("Resolving accounts with tag %s=%q…", tagKey, tagValue))
	} else {
		costStep(status, fmt.Sprintf("Resolving accounts with tag key %q…", tagKey))
	}

	payerNames := payerNameByAccountID(payers)
	seen := make(map[string]cost.AccountTarget)
	var targets []cost.AccountTarget
	var singleRoot string
	for _, payer := range payers {
		awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payer.AccountID, credentialsFile)
		if err != nil {
			return nil, err
		}
		matches, err := filterOrganizationAccountsByTag(ctx, awsCfg, payer.AccountID, tagKey, tagValue, status, configPath, sel.OrgCacheSkip, sel.OrgCacheRefresh)
		if err != nil {
			return nil, fmt.Errorf("list accounts by tag for payer %s: %w", payerErrorLabel(payer), err)
		}
		part := make([]cost.AccountTarget, 0, len(matches))
		for _, acct := range matches {
			displayAlias := cfg.AliasForAccountID(acct.ID)
			if displayAlias == acct.ID {
				displayAlias = ""
			}
			part = append(part, cost.AccountTarget{
				AccountID:        acct.ID,
				PayerAccountID:   payer.AccountID,
				ScopeAccountOnly: true,
				DisplayAlias:     displayAlias,
				DisplayName:      acct.Name,
			})
		}
		targets, err = appendCostTargets(targets, seen, part, payerNames)
		if err != nil {
			return nil, err
		}
		if len(payers) == 1 && len(matches) > 0 {
			rootID, err := organizationRootID(ctx, awsCfg)
			if err != nil {
				return nil, fmt.Errorf("resolve organization root for payer %s: %w", payerErrorLabel(payer), err)
			}
			singleRoot = rootID
		}
	}
	if len(targets) == 0 {
		if tagValue != "" {
			costStep(status, fmt.Sprintf("No accounts matched tag %s=%q", tagKey, tagValue))
		} else {
			costStep(status, fmt.Sprintf("No accounts matched tag key %q", tagKey))
		}
		return nil, nil
	}
	if len(payers) == 1 {
		sel.SelectionRootID = singleRoot
	}
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
