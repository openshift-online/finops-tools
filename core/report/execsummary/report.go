// report.go builds the PR1 executive summary report from collected monthly cost rows.
package execsummary

import (
	"fmt"
	"sort"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
)

// MonthSummary is the whole-organization monthly rollup with KPIs.
type MonthSummary struct {
	// Month is the YYYY-MM key.
	Month string
	// Total is the combined payer + linked account spend for the month.
	Total float64
	// DailyAverage is Total divided by the calendar days of the month.
	DailyAverage float64
	// MoMPct is the calendar-day-normalized month-over-month change in percent.
	// It is nil for the first displayed month and when the prior month's daily
	// average is zero (displayed as "N/A").
	MoMPct *float64
}

// ServiceMonthCost is one AWS service's cost for one account in one month.
type ServiceMonthCost struct {
	Month   string
	Service string
	Amount  float64
}

// AccountSummary is one account (payer or linked) in the report.
type AccountSummary struct {
	AccountID string
	// DisplayName is the Organizations account name, falling back to the account ID.
	DisplayName string
	// IsPayer marks the payer account's own spend.
	IsPayer bool
	// Total is the account's spend across the whole window.
	Total float64
	// MonthlyTotals maps YYYY-MM to the account's spend that month (missing = 0).
	MonthlyTotals map[string]float64
	// Services are the account's per-month AWS service rows.
	Services []ServiceMonthCost
}

// Report is the PR1 single-payer executive summary.
type Report struct {
	// PayerAlias is the configured payer alias (e.g. "rh-control").
	PayerAlias string
	// PayerAccountID is the 12-digit payer account ID.
	PayerAccountID string
	// Currency is the single billing currency.
	Currency string
	// StartMonth and EndMonth are the YYYY-MM window bounds (inclusive).
	StartMonth string
	EndMonth   string
	// Months holds every month in the window in order, with totals and MoM.
	Months []MonthSummary
	// Accounts holds payer and linked accounts, ordered by window spend descending.
	Accounts []AccountSummary
	// Rows are the raw (month, account, service) cells retained for drill-down tables.
	Rows []cost.MonthlyGroupedCost
}

// LatestMonth returns the most recent MonthSummary in the window.
func (r Report) LatestMonth() MonthSummary {
	if len(r.Months) == 0 {
		return MonthSummary{}
	}
	return r.Months[len(r.Months)-1]
}

// BuildReport aggregates a collected MonthlyAccountServiceResult over window into
// a Report. names maps account IDs to display names (missing keys fall back to the
// account ID). It fails when a cost row falls outside the window or when the result's
// payer has no matching account row set.
func BuildReport(
	payerAlias string,
	res cost.MonthlyAccountServiceResult,
	window TimeWindow,
	names map[string]string,
) (Report, error) {
	if len(window.Months) == 0 {
		return Report{}, fmt.Errorf("empty report window")
	}

	monthSet := make(map[string]time.Time, len(window.Months))
	for _, m := range window.Months {
		monthSet[MonthLabel(m)] = m
	}

	// Validate every row is inside the window before aggregating so partial or
	// misaligned data is never presented as complete.
	for _, row := range res.Rows {
		if _, ok := monthSet[row.Month]; !ok {
			return Report{}, fmt.Errorf("cost row for %s account %s service %q is outside the report window %s..%s",
				row.Month, row.AccountID, row.Service, MonthLabel(window.Start), MonthLabel(window.End))
		}
	}

	accounts := make(map[string]*AccountSummary)
	ensureAccount := func(id string) *AccountSummary {
		a, ok := accounts[id]
		if !ok {
			display := id
			if names != nil {
				if n, ok := names[id]; ok && n != "" {
					display = n
				}
			}
			a = &AccountSummary{
				AccountID:     id,
				DisplayName:   display,
				IsPayer:       id == res.PayerAccountID,
				MonthlyTotals: make(map[string]float64),
			}
			accounts[id] = a
		}
		return a
	}
	// The payer always appears, even with no own spend.
	ensureAccount(res.PayerAccountID)

	monthTotals := make(map[string]float64, len(window.Months))
	for _, row := range res.Rows {
		monthTotals[row.Month] += row.Amount
		a := ensureAccount(row.AccountID)
		a.MonthlyTotals[row.Month] += row.Amount
		a.Total += row.Amount
		a.Services = append(a.Services, ServiceMonthCost{Month: row.Month, Service: row.Service, Amount: row.Amount})
	}

	months := make([]MonthSummary, 0, len(window.Months))
	var prevAvg float64
	for i, m := range window.Months {
		label := MonthLabel(m)
		total := monthTotals[label]
		days := calendarDays(m)
		avg := 0.0
		if days > 0 {
			avg = total / float64(days)
		}
		ms := MonthSummary{Month: label, Total: total, DailyAverage: avg}
		if i > 0 && prevAvg > 0 {
			mom := (avg/prevAvg - 1) * 100
			ms.MoMPct = &mom
		}
		months = append(months, ms)
		prevAvg = avg
	}

	accountList := make([]AccountSummary, 0, len(accounts))
	for _, a := range accounts {
		sort.Slice(a.Services, func(i, j int) bool {
			if a.Services[i].Month != a.Services[j].Month {
				return a.Services[i].Month < a.Services[j].Month
			}
			return a.Services[i].Service < a.Services[j].Service
		})
		accountList = append(accountList, *a)
	}
	sort.Slice(accountList, func(i, j int) bool {
		if accountList[i].Total != accountList[j].Total {
			return accountList[i].Total > accountList[j].Total
		}
		return accountList[i].AccountID < accountList[j].AccountID
	})

	return Report{
		PayerAlias:     payerAlias,
		PayerAccountID: res.PayerAccountID,
		Currency:       res.Currency,
		StartMonth:     MonthLabel(window.Start),
		EndMonth:       MonthLabel(window.End),
		Months:         months,
		Accounts:       accountList,
		Rows:           res.Rows,
	}, nil
}

// calendarDays returns the number of calendar days in the month of t (UTC).
func calendarDays(t time.Time) int {
	return lastDayOfMonth(t).Day()
}

func lastDayOfMonth(t time.Time) time.Time {
	return firstOfMonth(t).AddDate(0, 1, 0).AddDate(0, 0, -1)
}
