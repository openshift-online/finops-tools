package extendedsupport

import (
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
)

type CostPeriodStatus string

const (
	CostPeriodMatchingCharges   CostPeriodStatus = "matching_charges"
	CostPeriodNoMatchingCharges CostPeriodStatus = "no_matching_charges"
	CostPeriodNotReturned       CostPeriodStatus = "no_period_returned"
	CostPeriodNotQueried        CostPeriodStatus = "not_queried"
)

// CostPeriodCoverage describes evidence returned under the report's billing
// filters. An absent period or empty groups do not establish missing history.
type CostPeriodCoverage struct {
	StartDate string           `json:"start_date"`
	EndDate   string           `json:"end_date"` // Inclusive.
	Status    CostPeriodStatus `json:"status"`
	// Incomplete means some cost queries returned this period and others omitted
	// it. Pages of a single query are combined before checking coverage.
	Incomplete bool `json:"incomplete,omitempty"`
}

type CostCoverage struct {
	Granularity        string               `json:"granularity"`
	TotalPeriods       int                  `json:"total_periods"`
	PeriodsWithRecords int                  `json:"periods_with_records"`
	Periods            []CostPeriodCoverage `json:"periods"`
	Notice             string               `json:"notice"`
}

type costCoverageTracker struct {
	coverage CostCoverage
	queries  int
	returned []int
	matched  []bool
}

func newCostCoverageTracker(q CostQuery) *costCoverageTracker {
	c := &costCoverageTracker{coverage: CostCoverage{
		Granularity: q.Granularity,
		Notice:      "Periods without matching records may have no charges or unavailable history. Totals include only returned billing records; coverage does not confirm historical data availability.",
	}}
	for start := q.Range.Start; start.Before(q.Range.End); {
		end := start.AddDate(0, 0, 1)
		if q.Granularity == "monthly" {
			end = time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		}
		if end.After(q.Range.End) {
			end = q.Range.End
		}
		c.coverage.Periods = append(c.coverage.Periods, CostPeriodCoverage{
			StartDate: cost.FormatDate(start), EndDate: cost.FormatDate(end.AddDate(0, 0, -1)),
		})
		start = end
	}
	c.coverage.TotalPeriods = len(c.coverage.Periods)
	c.returned = make([]int, c.coverage.TotalPeriods)
	c.matched = make([]bool, c.coverage.TotalPeriods)
	return c
}

func (c *costCoverageTracker) observe(returned []bool, start, end string, hasRecords bool) {
	for i, period := range c.coverage.Periods {
		if period.EndDate < start || period.StartDate > end {
			continue
		}
		returned[i] = true
		c.matched[i] = c.matched[i] || hasRecords
	}
}

func (c *costCoverageTracker) finishQuery(returned []bool) {
	c.queries++
	for i, seen := range returned {
		if seen {
			c.returned[i]++
		}
	}
}

func (c *costCoverageTracker) result() *CostCoverage {
	c.coverage.PeriodsWithRecords = 0
	for i := range c.coverage.Periods {
		period := &c.coverage.Periods[i]
		switch {
		case c.matched[i]:
			period.Status = CostPeriodMatchingCharges
			c.coverage.PeriodsWithRecords++
		case c.returned[i] > 0:
			period.Status = CostPeriodNoMatchingCharges
		case c.queries == 0:
			period.Status = CostPeriodNotQueried
		default:
			period.Status = CostPeriodNotReturned
		}
		period.Incomplete = c.returned[i] > 0 && c.returned[i] < c.queries
	}
	return &c.coverage
}
