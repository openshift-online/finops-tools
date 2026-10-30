package costpace

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
)

func TestBuildCalendarComparisons(t *testing.T) {
	tests := []struct {
		name, end, previousStart       string
		elapsed, monthDays, comparison int
		currentRate, previousRate      float64
	}{
		{"mid month", "2026-05-16", "2026-04-01", 15, 31, 15, 120, 100},
		{"short previous month", "2026-04-01", "2026-02-01", 31, 31, 28, 100, 100},
		{"leap February", "2024-04-01", "2024-02-01", 31, 31, 29, 100, 100},
		{"short current month", "2026-05-01", "2026-03-01", 30, 30, 30, 100, 100},
		{"year boundary", "2026-01-16", "2025-12-01", 15, 31, 15, 80, 100},
		{"first day", "2026-05-02", "2026-04-01", 1, 31, 1, 120, 100},
		{"zero baseline", "2026-05-16", "2026-04-01", 15, 31, 15, 100, 0},
		{"negative baseline", "2026-05-16", "2026-04-01", 15, 31, 15, 100, -10},
		{"zero spend", "2026-05-16", "2026-04-01", 15, 31, 15, 0, 100},
		{"net credits", "2026-05-16", "2026-04-01", 15, 31, 15, -10, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			end := parseDate(t, tt.end)
			q := cost.CostQuery{
				Accounts: []cost.AccountTarget{{AccountID: "111111111111", PayerAccountID: "123456789012"}},
				// A short input range must not truncate the previous month.
				Range: cost.DateRange{Start: end.AddDate(0, 0, -1), End: end}, Workers: 3,
			}
			fetch := func(_ context.Context, got cost.CostQuery) ([]cost.DailyCostItem, string, error) {
				if cost.FormatDate(got.Range.Start) != tt.previousStart || !got.Range.End.Equal(end) {
					t.Fatalf("fetch range = %+v", got.Range)
				}
				if !got.RequireDailyCoverage || got.Workers != 3 || !reflect.DeepEqual(got.Accounts, q.Accounts) {
					t.Fatal("fetch must preserve account scope/workers and require daily coverage")
				}
				return dailyCosts(got.Range, tt.currentRate, tt.previousRate), "USD", nil
			}
			r, err := buildWith(t.Context(), q, end.AddDate(0, 0, 2), fetch)
			if err != nil {
				t.Fatal(err)
			}
			if r.DaysElapsed != tt.elapsed || r.DaysInMonth != tt.monthDays || r.ComparisonDays != tt.comparison {
				t.Fatalf("calendar fields = %+v", r)
			}
			checkClose(t, r.CurrentMTD, tt.currentRate*float64(tt.elapsed))
			checkClose(t, r.CurrentComparable, tt.currentRate*float64(tt.comparison))
			checkClose(t, r.PreviousComparable, tt.previousRate*float64(tt.comparison))
			checkClose(t, r.DailyAverage, tt.currentRate)
			checkClose(t, r.ProjectedTotal, tt.currentRate*float64(tt.monthDays))
			previousDays := r.PreviousPeriod.End.AddDate(0, 0, -1).Day()
			checkClose(t, r.PreviousFullMonth, tt.previousRate*float64(previousDays))
			if tt.previousRate <= 0 {
				if r.PacePercent != nil || r.ProjectedChangePercent != nil {
					t.Fatal("nonpositive baseline must have unavailable percentages")
				}
			} else {
				if r.PacePercent == nil || r.ProjectedChangePercent == nil {
					t.Fatal("missing percentages")
				}
				checkClose(t, *r.PacePercent, (tt.currentRate-tt.previousRate)/tt.previousRate*100)
				checkClose(t, *r.ProjectedChangePercent, (r.ProjectedTotal-r.PreviousFullMonth)/r.PreviousFullMonth*100)
			}
			if r.Currency != "USD" || r.Metric != cost.MetricNetAmortized {
				t.Fatalf("metadata = %+v", r)
			}
		})
	}
}

func TestBuildRejectsIncompleteOrInvalidData(t *testing.T) {
	end := parseDate(t, "2026-05-16")
	q := cost.CostQuery{Accounts: []cost.AccountTarget{{AccountID: "123456789012"}}, Range: cost.DateRange{End: end}}
	tests := []struct {
		name   string
		change func([]cost.DailyCostItem) []cost.DailyCostItem
		want   string
	}{
		{"missing previous day", func(d []cost.DailyCostItem) []cost.DailyCostItem { return d[1:] }, "unavailable for 2026-04-01"},
		{"missing latest day", func(d []cost.DailyCostItem) []cost.DailyCostItem { return d[:len(d)-1] }, "unavailable for 2026-05-15"},
		{"missing current month", func(d []cost.DailyCostItem) []cost.DailyCostItem { return d[:30] }, "unavailable for 2026-05-01"},
		{"no data", func([]cost.DailyCostItem) []cost.DailyCostItem { return nil }, "unavailable"},
		{"duplicate", func(d []cost.DailyCostItem) []cost.DailyCostItem { return append(d, d[0]) }, "duplicate"},
		{"NaN", func(d []cost.DailyCostItem) []cost.DailyCostItem { d[0].Amount = math.NaN(); return d }, "invalid daily cost"},
		{"infinity", func(d []cost.DailyCostItem) []cost.DailyCostItem { d[0].Amount = math.Inf(1); return d }, "invalid daily cost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildWith(t.Context(), q, end, func(_ context.Context, got cost.CostQuery) ([]cost.DailyCostItem, string, error) {
				return tt.change(dailyCosts(got.Range, 100, 100)), "USD", nil
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildDefaultCutoffUsesUTCAndLastIncludedMonth(t *testing.T) {
	// Still May 31 locally, but June 1 UTC: report the complete May month.
	now := time.Date(2026, 5, 31, 22, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	r, err := buildWith(t.Context(), cost.CostQuery{Accounts: []cost.AccountTarget{{AccountID: "123456789012"}}}, now,
		func(_ context.Context, q cost.CostQuery) ([]cost.DailyCostItem, string, error) {
			if cost.FormatDate(q.Range.End) != "2026-06-01" {
				t.Fatalf("cutoff = %v", q.Range.End)
			}
			return dailyCosts(q.Range, 100, 100), "USD", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if r.DaysElapsed != 31 || cost.FormatDate(r.CurrentPeriod.Start) != "2026-05-01" {
		t.Fatalf("report = %+v", r)
	}
}

func TestBuildValidationAndFetchErrors(t *testing.T) {
	now := parseDate(t, "2026-05-16")
	fetchErr := errors.New("access denied")
	fetch := func(context.Context, cost.CostQuery) ([]cost.DailyCostItem, string, error) { return nil, "", fetchErr }
	q := cost.CostQuery{Accounts: []cost.AccountTarget{{AccountID: "123456789012"}}}
	if _, err := buildWith(t.Context(), q, now, fetch); !errors.Is(err, fetchErr) {
		t.Fatalf("error = %v", err)
	}
	noFetch := func(context.Context, cost.CostQuery) ([]cost.DailyCostItem, string, error) {
		t.Fatal("invalid input must not fetch")
		return nil, "", nil
	}
	if _, err := buildWith(t.Context(), cost.CostQuery{}, now, noFetch); err == nil {
		t.Fatal("expected account validation error")
	}
	q.Range.End = now.AddDate(0, 0, 1)
	if _, err := buildWith(t.Context(), q, now, noFetch); err == nil {
		t.Fatal("expected future cutoff error")
	}
}

func dailyCosts(dr cost.DateRange, currentRate, previousRate float64) []cost.DailyCostItem {
	last := dr.End.AddDate(0, 0, -1)
	var out []cost.DailyCostItem
	for day := dr.Start; day.Before(dr.End); day = day.AddDate(0, 0, 1) {
		amount := previousRate
		if day.Month() == last.Month() && day.Year() == last.Year() {
			amount = currentRate
		}
		out = append(out, cost.DailyCostItem{Date: cost.FormatDate(day), Amount: amount})
	}
	return out
}

func parseDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := cost.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func checkClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-8 {
		t.Errorf("got %.8f, want %.8f", got, want)
	}
}
