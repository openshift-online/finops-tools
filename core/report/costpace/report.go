// Package costpace compares month-to-date net amortized spend with the same
// number of days in the previous month and projects a full-month total.
package costpace

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
)

// Report separates the complete month-to-date total from the equal-length
// comparison windows. Date ranges have inclusive starts and exclusive ends.
type Report struct {
	GeneratedAt    time.Time
	Currency       string
	Metric         string
	CurrentPeriod  cost.DateRange
	PreviousPeriod cost.DateRange
	DaysElapsed    int
	DaysInMonth    int
	ComparisonDays int

	CurrentMTD         float64
	CurrentComparable  float64
	PreviousComparable float64
	PreviousFullMonth  float64
	DailyAverage       float64
	ProjectedTotal     float64
	// Percentages are unavailable when the previous baseline is zero or
	// negative. Credits still contribute to all totals and the projection.
	PacePercent            *float64
	ProjectedChangePercent *float64
}

// Build uses q.Range.End as the exclusive reporting cutoff, defaulting to
// today's UTC midnight. The last included day determines the reporting month.
// It derives calendar-month windows independently of q.Range.Start, fetching
// the entire previous month and the reporting month through the cutoff.
// The projection is a linear daily run rate, not an AWS forecast or a
// statistical confidence estimate. Already reported AWS costs may be revised.
func Build(ctx context.Context, q cost.CostQuery, now time.Time) (Report, error) {
	return buildWith(ctx, q, now, cost.FetchDaily)
}

type fetchDailyFunc func(context.Context, cost.CostQuery) ([]cost.DailyCostItem, string, error)

func buildWith(ctx context.Context, q cost.CostQuery, now time.Time, fetch fetchDailyFunc) (Report, error) {
	if len(q.Accounts) == 0 {
		return Report{}, fmt.Errorf("at least one account is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	today := dateOnly(now)
	end := q.Range.End
	if end.IsZero() {
		end = today
	}
	end = dateOnly(end)
	if end.After(today) {
		return Report{}, fmt.Errorf("reporting cutoff must exclude today and future dates")
	}
	lastDay := end.AddDate(0, 0, -1)
	monthStart := time.Date(lastDay.Year(), lastDay.Month(), 1, 0, 0, 0, 0, time.UTC)
	previousStart := monthStart.AddDate(0, -1, 0)
	daysInMonth := monthStart.AddDate(0, 1, -1).Day()
	daysInPrevious := monthStart.AddDate(0, 0, -1).Day()
	comparisonDays := min(lastDay.Day(), daysInPrevious)

	q.Range = cost.DateRange{Start: previousStart, End: end}
	q.RequireDailyCoverage = true
	daily, currency, err := fetch(ctx, q)
	if err != nil {
		return Report{}, fmt.Errorf("fetch cost pace: %w", err)
	}
	amounts := make(map[string]float64, len(daily))
	for _, item := range daily {
		if math.IsNaN(item.Amount) || math.IsInf(item.Amount, 0) {
			return Report{}, fmt.Errorf("invalid daily cost for %s", item.Date)
		}
		if _, duplicate := amounts[item.Date]; duplicate {
			return Report{}, fmt.Errorf("duplicate daily cost for %s", item.Date)
		}
		amounts[item.Date] = item.Amount
	}
	r := Report{
		GeneratedAt: now.UTC(), Currency: currency, Metric: cost.MetricNetAmortized,
		CurrentPeriod:  cost.DateRange{Start: monthStart, End: end},
		PreviousPeriod: cost.DateRange{Start: previousStart, End: monthStart},
		DaysElapsed:    lastDay.Day(), DaysInMonth: daysInMonth, ComparisonDays: comparisonDays,
	}
	for day := previousStart; day.Before(end); day = day.AddDate(0, 0, 1) {
		amount, present := amounts[cost.FormatDate(day)]
		if !present {
			return Report{}, fmt.Errorf("daily cost data unavailable for %s", cost.FormatDate(day))
		}
		if day.Before(monthStart) {
			r.PreviousFullMonth += amount
			if day.Day() <= comparisonDays {
				r.PreviousComparable += amount
			}
		} else {
			r.CurrentMTD += amount
			if day.Day() <= comparisonDays {
				r.CurrentComparable += amount
			}
		}
	}
	r.DailyAverage = r.CurrentMTD / float64(r.DaysElapsed)
	r.ProjectedTotal = r.DailyAverage * float64(r.DaysInMonth)
	r.PacePercent = percentageChange(r.CurrentComparable, r.PreviousComparable)
	r.ProjectedChangePercent = percentageChange(r.ProjectedTotal, r.PreviousFullMonth)
	return r, nil
}

func percentageChange(current, previous float64) *float64 {
	if previous <= 0 {
		return nil
	}
	value := (current - previous) / previous * 100
	return &value
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
