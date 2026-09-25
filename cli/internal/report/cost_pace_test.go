package report

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/report/costpace"
)

func costPaceFixture() costpace.Report {
	march := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	zero := 0.0
	return costpace.Report{
		GeneratedAt: march.AddDate(0, 1, 0), Currency: "USD", Metric: cost.MetricNetAmortized,
		CurrentPeriod:  cost.DateRange{Start: march, End: march.AddDate(0, 1, 0)},
		PreviousPeriod: cost.DateRange{Start: march.AddDate(0, -1, 0), End: march},
		DaysElapsed:    31, DaysInMonth: 31, ComparisonDays: 28,
		CurrentMTD: 3100, CurrentComparable: 2800, PreviousComparable: 2800,
		PreviousFullMonth: 2800, DailyAverage: 100, ProjectedTotal: 3100, PacePercent: &zero,
	}
}

func TestCostPaceHTMLDatesAmountsAndEscaping(t *testing.T) {
	var out strings.Builder
	if err := RenderCostPaceHTML(&out, costPaceFixture(), "<script>alert(1)</script>"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<!DOCTYPE html>", "Cost Pace — Mar 2026", "Data through:</strong> 2026-03-31",
		"2026-03-01 — 2026-03-28", "2026-02-01 — 2026-02-28", "USD 3,100.00", "USD 2,800.00",
		"Unchanged spend", "previous month is shorter", "N/A", "&lt;script&gt;",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in HTML", want)
		}
	}
	if strings.Contains(out.String(), "<script>") {
		t.Fatal("account name was not escaped")
	}
}

func TestCostPaceHTMLDirectionsAndCredits(t *testing.T) {
	positive, negative := 12.5, -5.0
	for _, tc := range []struct {
		pct  *float64
		want string
	}{
		{nil, "Unavailable"}, {&positive, "Higher spend"}, {&negative, "Lower spend"},
	} {
		r := costPaceFixture()
		r.PacePercent, r.CurrentMTD, r.ProjectedTotal = tc.pct, -310, -310
		var out strings.Builder
		if err := RenderCostPaceHTML(&out, r, "Test payer"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), tc.want) || !strings.Contains(out.String(), "USD -310.00") {
			t.Fatalf("missing direction or credits: %s", out.String())
		}
	}
}

type costPaceErrorWriter struct{}

func (costPaceErrorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCostPaceHTMLPropagatesWriteError(t *testing.T) {
	if err := RenderCostPaceHTML(costPaceErrorWriter{}, costPaceFixture(), "Test payer"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("error = %v", err)
	}
}

func TestCostPaceGeneratorPreservesCutoffAndScope(t *testing.T) {
	var out strings.Builder
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	dr, err := cost.ResolvePeriod(cost.PeriodSpec{Days: 30, ExcludeRecentDays: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	targets := []cost.AccountTarget{{AccountID: "111111111111", PayerAccountID: "123456789012", ScopeAccountOnly: true}}
	called := false
	g := costPaceGenerator{build: func(_ context.Context, q cost.CostQuery, gotNow time.Time) (costpace.Report, error) {
		called = true
		if q.Range != dr || cost.FormatDate(q.Range.End) != "2026-09-23" || !reflect.DeepEqual(q.Accounts, targets) || q.Workers != 4 || q.Provider != cost.ProviderAWS || !gotNow.Equal(now) {
			t.Fatalf("query = %+v, now = %v", q, gotNow)
		}
		return costPaceFixture(), nil
	}}
	err = g.Generate(t.Context(), GenerateInput{Format: FormatHTML, Out: &out, Targets: targets, Range: dr, Now: now, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !called || !strings.Contains(out.String(), "Cost Pace") {
		t.Fatal("generator did not build and render")
	}
}

func TestCostPaceGeneratorErrors(t *testing.T) {
	for _, in := range []GenerateInput{{Format: FormatHTML}, {Format: "json", Targets: []cost.AccountTarget{{AccountID: "123456789012"}}}} {
		if err := (costPaceGenerator{}).Validate(in); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	want := errors.New("missing daily cost")
	g := costPaceGenerator{build: func(context.Context, cost.CostQuery, time.Time) (costpace.Report, error) {
		return costpace.Report{}, want
	}}
	var out strings.Builder
	err := g.Generate(t.Context(), GenerateInput{Format: FormatHTML, Out: &out, Targets: []cost.AccountTarget{{AccountID: "123456789012"}}})
	if !errors.Is(err, want) || out.Len() != 0 {
		t.Fatalf("error = %v, output = %q", err, out.String())
	}
}
