package extendedsupport

import (
	"context"
	"sort"
	"time"

	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/openshift-online/finops-tools/core/cost"
)

const chargesWithoutFindingsDays = 5

type AccountChargesWithoutFindings struct {
	AccountID      string  `json:"account_id"`
	DisplayAlias   string  `json:"display_alias,omitempty"`
	Amount         float64 `json:"amount"`
	Currency       string  `json:"currency"`
	ScanIncomplete bool    `json:"scan_incomplete,omitempty"`
}

// ChargesWithoutFindingsSummary identifies historical billing that the current
// resource findings do not explain. It is a discovery lead, not proof that a
// resource is still running or that all discovery operations succeeded.
type ChargesWithoutFindingsSummary struct {
	ListCostSummary
	Accounts []AccountChargesWithoutFindings `json:"accounts"`
}

// AccountsWithoutFindings selects from accounts submitted to the scan, after
// retries. Entirely skipped accounts stay in SkippedAccounts, never this report.
// Past-support/unknown-status records do not establish current support findings.
func AccountsWithoutFindings(targets []cost.AccountTarget, result Result) []cost.AccountTarget {
	excluded := make(map[string]bool)
	for _, record := range result.Records {
		excluded[record.AccountID] = true
	}
	for _, warning := range result.Summary.SkippedAccounts {
		excluded[warning.AccountID] = true
	}
	var out []cost.AccountTarget
	for _, target := range targets {
		if !excluded[target.AccountID] {
			out = append(out, target)
			excluded[target.AccountID] = true
		}
	}
	return out
}

// FindChargesWithoutFindings checks the last five complete UTC days using the
// billing region scope as discovery, across every service. The service selection
// for resource discovery is deliberately ignored. Only positive totals appear.
// Failed billing sources produce warnings and no partial or invented amounts.
func FindChargesWithoutFindings(ctx context.Context, q ListCostQuery, result Result) (Result, error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if q.Now.IsZero() {
		q.Now = time.Now()
	}
	period, err := cost.ResolvePeriod(cost.PeriodSpec{Days: chargesWithoutFindingsDays}, q.Now)
	if err != nil {
		return result, err
	}
	metadata := &ChargesWithoutFindingsSummary{
		ListCostSummary: ListCostSummary{
			Start: cost.FormatDate(period.Start), EndExclusive: cost.FormatDate(period.End), Metric: listCostMetric,
			AccountIDs: []string{}, Services: []Service{}, AllServices: true, Regions: awsregions.Normalize(q.Regions), ExcludedRegions: awsregions.Normalize(q.SkipRegions),
			Notice: "Positive Extended Support charges across all services in the last five complete UTC days, but no current Extended Support resources were found by the selected resource scans. Billing ignores --services and honors region filters, including charges for services not yet implemented in discovery. Charges can also reflect upgraded/deleted resources. The latest two days may be incomplete; review scan warnings.",
		},
		Accounts: []AccountChargesWithoutFindings{},
	}
	result.Summary.ChargesWithoutFindings = metadata
	targets := AccountsWithoutFindings(q.Targets, result)
	aliases := make(map[string]string)
	for i := range targets {
		targets[i].ScopeAccountOnly = true
		metadata.AccountIDs = append(metadata.AccountIDs, targets[i].AccountID)
		aliases[targets[i].AccountID] = targets[i].DisplayAlias
	}
	sort.Strings(metadata.AccountIDs)
	incomplete := make(map[string]bool)
	for _, warning := range result.Summary.SkippedRegions {
		incomplete[warning.AccountID] = true
	}
	for _, resource := range result.Summary.UnclassifiedResources {
		incomplete[resource.AccountID] = true
	}
	groups := cost.GroupByCredentialsAccount(targets)
	for _, source := range cost.SortedCredentialAccountIDs(groups) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		billing, err := FetchCosts(ctx, CostQuery{
			Targets: groups[source], Range: period, Regions: q.Regions, SkipRegions: q.SkipRegions,
			GroupBy: CostGroupByAccount, Granularity: "daily", TotalsOnly: true, Metric: listCostMetric, allServices: true, newClient: q.newClient,
		})
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			metadata.Warnings = append(metadata.Warnings, err.Error())
			continue
		}
		for _, row := range billing.Rows {
			if row.Amount > 0 {
				metadata.Accounts = append(metadata.Accounts, AccountChargesWithoutFindings{
					AccountID: row.AccountID, DisplayAlias: aliases[row.AccountID], Amount: row.Amount, Currency: billing.Currency,
					ScanIncomplete: incomplete[row.AccountID] || len(result.Summary.LifecycleWarnings) > 0,
				})
			}
		}
	}
	sort.Slice(metadata.Accounts, func(i, j int) bool { return metadata.Accounts[i].AccountID < metadata.Accounts[j].AccountID })
	return result, nil
}
