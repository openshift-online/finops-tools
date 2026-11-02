// monthly_account_service.go collects monthly per-account/per-service costs for one payer
// (payer own spend plus linked member accounts) with reconciliation.
package cost

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// reconciliationTolerance is the sub-cent tolerance for representation noise when
// reconciling grouped account/service sums against monthly totals.
const reconciliationTolerance = 0.01

// MonthlyAccountServiceResult is the collected monthly cost data for one payer
// and its selected linked accounts.
type MonthlyAccountServiceResult struct {
	// Rows are the (month, linked account, AWS service) cost cells, deterministically ordered.
	Rows []MonthlyGroupedCost
	// Currency is the single billing currency across all rows.
	Currency string
	// PayerAccountID is the payer whose credentials served the queries.
	PayerAccountID string
}

// FetchMonthlyAccountService retrieves monthly net amortized cost by linked account and
// AWS service for one payer and its linked member accounts over q.Range (month-aligned
// half-open). The payer's own spend is always collected through a separate payer-ID-filtered
// query so it is included exactly once, and grouped sums are reconciled per month against
// monthly totals for the selected accounts within a sub-cent tolerance.
//
// It fails on: multiple payers or credentials, mixed currencies, Cost Explorer errors,
// or reconciliation differences beyond the tolerance (which includes payer own spend
// that the payer-ID filter cannot return).
func FetchMonthlyAccountService(ctx context.Context, q CostQuery) (MonthlyAccountServiceResult, error) {
	return fetchMonthlyAccountServiceWith(ctx, q, fetchAWSOptions{
		Now:             time.Now(),
		NewCostExplorer: defaultCostExplorerFactory(),
	})
}

func fetchMonthlyAccountServiceWith(ctx context.Context, q CostQuery, opts fetchAWSOptions) (MonthlyAccountServiceResult, error) {
	if len(q.Accounts) == 0 {
		return MonthlyAccountServiceResult{}, errors.New("at least one account is required")
	}
	if opts.NewCostExplorer == nil {
		opts.NewCostExplorer = defaultCostExplorerFactory()
	}

	payer, members, err := splitPayerAndMembers(q.Accounts)
	if err != nil {
		return MonthlyAccountServiceResult{}, err
	}

	cfg := payer.AWSConfig
	if cfg.Region == "" {
		cfg.Region = CostExplorerRegion
	}
	ce := opts.NewCostExplorer(cfg)
	dr := monthlyCERange(EffectiveRange(q, opts.Now))

	if q.Progress != nil {
		q.Progress.Step(fmt.Sprintf("Fetching monthly account/service costs for payer %s and %d linked account(s)…",
			payer.AccountID, len(members)))
	}

	var (
		rows     []MonthlyGroupedCost
		currency string
	)

	// Member batches exclude the payer so its own spend is only ever counted from
	// the explicit payer-ID query below.
	memberIDs := sortedTargetAccountIDs(members)
	for _, batch := range batchStrings(memberIDs, linkedAccountFilterBatchSize) {
		batchRows, batchCurrency, err := sumNetAmortizedMonthlyGrouped(ctx, ce, dr, linkedAccountsFilter(batch))
		if err != nil {
			return MonthlyAccountServiceResult{}, fmt.Errorf("member accounts query: %w", err)
		}
		currency, err = mergeCurrency(currency, batchCurrency)
		if err != nil {
			return MonthlyAccountServiceResult{}, err
		}
		rows = append(rows, batchRows...)
	}

	payerRows, payerCurrency, err := sumNetAmortizedMonthlyGrouped(ctx, ce, dr, LinkedAccountFilter(payer.AccountID, true))
	if err != nil {
		return MonthlyAccountServiceResult{}, fmt.Errorf("payer %s own spend query: %w", payer.AccountID, err)
	}
	currency, err = mergeCurrency(currency, payerCurrency)
	if err != nil {
		return MonthlyAccountServiceResult{}, err
	}
	rows = append(rows, payerRows...)

	// Monthly totals scoped to the selected accounts (payer + members).
	selectedIDs := append([]string{payer.AccountID}, memberIDs...)
	months, totalsCurrency, _, err := sumNetAmortizedMonthly(ctx, ce, dr, linkedAccountsFilter(selectedIDs))
	if err != nil {
		return MonthlyAccountServiceResult{}, fmt.Errorf("monthly totals query: %w", err)
	}
	currency, err = mergeCurrency(currency, totalsCurrency)
	if err != nil {
		return MonthlyAccountServiceResult{}, err
	}

	if err := reconcileMonthly(rows, months); err != nil {
		return MonthlyAccountServiceResult{}, err
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Month != rows[j].Month {
			return rows[i].Month < rows[j].Month
		}
		if rows[i].AccountID != rows[j].AccountID {
			return rows[i].AccountID < rows[j].AccountID
		}
		return rows[i].Service < rows[j].Service
	})

	return MonthlyAccountServiceResult{
		Rows:           rows,
		Currency:       currency,
		PayerAccountID: payer.AccountID,
	}, nil
}

// splitPayerAndMembers validates that all targets share one credentials (payer)
// account and returns the payer target plus the linked member targets.
func splitPayerAndMembers(targets []AccountTarget) (AccountTarget, []AccountTarget, error) {
	var payer AccountTarget
	var members []AccountTarget
	for _, t := range targets {
		if strings.TrimSpace(t.AccountID) == "" {
			return AccountTarget{}, nil, errors.New("account target with empty account ID")
		}
		if t.IsLinked() {
			members = append(members, t)
			continue
		}
		if payer.AccountID != "" {
			return AccountTarget{}, nil, fmt.Errorf("executive summary requires exactly one payer target, found %s and %s",
				payer.AccountID, t.AccountID)
		}
		payer = t
	}
	if payer.AccountID == "" {
		return AccountTarget{}, nil, fmt.Errorf("executive summary requires a payer target (none of %d targets is an unlinked payer account)", len(targets))
	}
	for _, m := range members {
		if m.PayerAccountID != payer.AccountID {
			return AccountTarget{}, nil, fmt.Errorf("executive summary requires a single payer: member %s is linked to payer %s, not %s",
				m.AccountID, m.PayerAccountID, payer.AccountID)
		}
	}
	return payer, members, nil
}

func sortedTargetAccountIDs(targets []AccountTarget) []string {
	ids := make([]string, 0, len(targets))
	for _, t := range targets {
		ids = append(ids, strings.TrimSpace(t.AccountID))
	}
	sort.Strings(ids)
	return ids
}

// mergeCurrency enforces a single billing currency across all queries of one report.
func mergeCurrency(existing, next string) (string, error) {
	if next == "" {
		return existing, nil
	}
	if existing == "" {
		return next, nil
	}
	if existing != next {
		return "", fmt.Errorf("cannot merge monthly account/service results with mixed currency (%s vs %s)", existing, next)
	}
	return existing, nil
}

// reconcileMonthly verifies that grouped (account, service) sums match the monthly
// totals for the selected accounts within the sub-cent tolerance, month by month.
func reconcileMonthly(rows []MonthlyGroupedCost, months []MonthlyCostPoint) error {
	groupedByMonth := make(map[string]float64)
	for _, r := range rows {
		groupedByMonth[r.Month] += r.Amount
	}
	totalsByMonth := make(map[string]float64)
	for _, m := range months {
		totalsByMonth[m.Month] = m.Amount
	}

	allMonths := make(map[string]struct{}, len(totalsByMonth)+len(groupedByMonth))
	for m := range totalsByMonth {
		allMonths[m] = struct{}{}
	}
	for m := range groupedByMonth {
		allMonths[m] = struct{}{}
	}
	sorted := make([]string, 0, len(allMonths))
	for m := range allMonths {
		sorted = append(sorted, m)
	}
	sort.Strings(sorted)

	for _, month := range sorted {
		diff := groupedByMonth[month] - totalsByMonth[month]
		if diff < 0 {
			diff = -diff
		}
		if diff > reconciliationTolerance {
			return fmt.Errorf(
				"monthly account/service costs do not reconcile for %s: grouped %.4f vs total %.4f (difference %.4f exceeds %.2f; payer own spend or account/service rows may be missing)",
				month, groupedByMonth[month], totalsByMonth[month], diff, reconciliationTolerance)
		}
	}
	return nil
}
