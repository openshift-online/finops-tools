package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/spf13/cobra"
)

var discoverExtendedSupportBillingCandidates = extendedsupport.DiscoverBillingCandidates

func filterExtendedSupportBillingTargets(
	cmd *cobra.Command,
	cfg configstore.File,
	sel costTargetSelector,
	targets []cost.AccountTarget,
	fullScan bool,
	lookbackDays int,
	now time.Time,
	status costStepper,
) ([]cost.AccountTarget, *extendedsupport.BillingSelection, error) {
	mode, err := validateCostTargetSelector(sel)
	if err != nil {
		return nil, nil, err
	}
	if fullScan || mode != costTargetModeOrg {
		return targets, nil, nil
	}
	period, err := extendedsupport.BillingLookbackRange(now, lookbackDays)
	if err != nil {
		return nil, nil, err
	}
	payers, err := resolveSelectorPayers(cfg, sel.PayerAliases)
	if err != nil {
		return nil, nil, err
	}
	ctx := awsCommandContext(cmd)
	// Payer-wide target resolution has already ensured each payer's credentials.
	costStep(status, "Finding accounts with billed Extended Support usage…")
	billed := make(map[string]bool)
	accountUsageTypes := make(map[string]map[string]bool)
	usageSeen := make(map[string]struct{})
	usageTypes := make([]string, 0)
	payerIDs := make([]string, 0, len(payers))
	for _, payer := range payers {
		awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payer.AccountID, awsFlags.CredentialsFile)
		if err != nil {
			return nil, nil, fmt.Errorf("load payer %s for Cost Explorer account discovery: %w", payerErrorLabel(payer), err)
		}
		candidates, err := discoverExtendedSupportBillingCandidates(ctx, awsCfg, period)
		if err != nil {
			return nil, nil, fmt.Errorf("cost explorer account discovery failed for payer %s: %w (use --full-scan to scan all active members)", payerErrorLabel(payer), err)
		}
		payerIDs = append(payerIDs, payer.AccountID)
		for _, id := range candidates.AccountIDs {
			billed[id] = true
			if accountUsageTypes[id] == nil {
				accountUsageTypes[id] = make(map[string]bool)
			}
			for _, usage := range candidates.AccountUsageTypes[id] {
				accountUsageTypes[id][usage] = true
			}
		}
		for _, usageType := range candidates.UsageTypes {
			if _, ok := usageSeen[usageType]; ok {
				continue
			}
			usageSeen[usageType] = struct{}{}
			usageTypes = append(usageTypes, usageType)
		}
	}
	selected := make([]cost.AccountTarget, 0, len(targets))
	selectedAccounts := make([]extendedsupport.BillingSelectedAccount, 0)
	for _, target := range targets {
		if billed[target.AccountID] {
			selected = append(selected, target)
			account := extendedsupport.BillingSelectedAccount{
				AccountID: target.AccountID, DisplayName: target.DisplayName, DisplayAlias: target.DisplayAlias, UsageTypes: []string{},
			}
			for usage := range accountUsageTypes[target.AccountID] {
				account.UsageTypes = append(account.UsageTypes, usage)
			}
			sort.Strings(account.UsageTypes)
			selectedAccounts = append(selectedAccounts, account)
		}
	}
	sort.Slice(selectedAccounts, func(i, j int) bool { return selectedAccounts[i].AccountID < selectedAccounts[j].AccountID })
	selection := &extendedsupport.BillingSelection{
		PayerAccountIDs: payerIDs,
		Start:           period.Start.Format("2006-01-02"), EndExclusive: period.End.Format("2006-01-02"),
		UsageTypes: usageTypes, BilledAccountCount: len(billed),
		SelectedAccounts:     selectedAccounts,
		EligibleAccountCount: len(targets), SelectedAccountCount: len(selected),
		Notice: "Billing data can be delayed; accounts without matching recorded usage in this window are not scanned. Usage-type matches select accounts even when recorded charges are zero or negative. Initial account selection is payer-wide; service and region flags limit resource scans and 30-day cost summaries. The five-day check covers all services and honors region flags.",
	}
	if len(payerIDs) == 1 {
		selection.PayerAccountID = payerIDs[0]
	}
	costStep(status, fmt.Sprintf("Cost Explorer selected %d of %d eligible member accounts (%d matching usage types)…", len(selected), len(targets), len(selection.UsageTypes)))
	return selected, selection, nil
}
