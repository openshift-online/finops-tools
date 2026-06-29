package report

import (
	"testing"

	coresp "github.com/openshift-online/finops-tools/core/report/savingsplans"
)

func TestMonthDisplayLabel(t *testing.T) {
	tests := []struct {
		month, start, end, want string
	}{
		{"2026-02", "2026-01-01", "2026-06-30", "2026-02"},
		{"2026-01", "2026-01-01", "2026-06-30", "2026-01"},
		{"2026-01", "2026-01-15", "2026-06-08", "2026-01 (from 15)"},
		{"2026-06", "2026-01-15", "2026-06-08", "2026-06 (through 8)"},
		{"2026-03", "2026-01-15", "2026-06-08", "2026-03"},
		{"2026-01", "2026-01-15", "2026-01-28", "2026-01 (15 – 28)"},
		{"2026-02", "2026-02-01", "2026-02-28", "2026-02"},
		{"2026-02", "2026-02-01", "2026-02-15", "2026-02 (through 15)"},
	}
	for _, tt := range tests {
		got := monthDisplayLabel(tt.month, tt.start, tt.end)
		if got != tt.want {
			t.Errorf("monthDisplayLabel(%q, %q, %q) = %q, want %q", tt.month, tt.start, tt.end, got, tt.want)
		}
	}
}

func TestPopulateDashboard_avgCoverageExcludesInvalidCoverageOnDemand(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{
			{
				AccountName:     "With coverage",
				CoverageAverage: coresp.PeriodAverage{Percentage: 90.0, OK: true},
				SavingsTotal:    coresp.PeriodSavings{NetSavings: 1000, OnDemandCostEquivalent: 10000, OK: true},
			},
			{
				AccountName:  "No coverage data",
				SavingsTotal: coresp.PeriodSavings{NetSavings: 5000, OnDemandCostEquivalent: 90000, OK: true},
			},
		},
	})
	if view.AvgCoverageFormatted != "90.0%" {
		t.Errorf("AvgCoverageFormatted = %q, want 90.0%%", view.AvgCoverageFormatted)
	}
}

func TestMonthDisplayLabel_invalidDates(t *testing.T) {
	if got := monthDisplayLabel("2026-01", "bad", "2026-06-08"); got != "2026-01" {
		t.Errorf("got %q", got)
	}
}

func TestPopulateDashboard_missingMetricsShowEmptyDashboardFields(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{
			{
				AccountName: "No metrics",
			},
			{
				AccountName:        "Partial metrics",
				UtilizationAverage: coresp.PeriodAverage{Percentage: 88.0, OK: true},
			},
		},
	})
	if len(view.AccountRows) != 2 {
		t.Fatalf("AccountRows len = %d, want 2", len(view.AccountRows))
	}

	noMetrics := view.AccountRows[0]
	if noMetrics.CoverageFormatted != "" || noMetrics.CoverageStatus != "" || noMetrics.CoverageStatusClass != "" {
		t.Errorf("missing coverage should be empty, got formatted=%q status=%q class=%q",
			noMetrics.CoverageFormatted, noMetrics.CoverageStatus, noMetrics.CoverageStatusClass)
	}
	if noMetrics.UtilizationFormatted != "" || noMetrics.UtilizationStatus != "" {
		t.Errorf("missing utilization should be empty, got formatted=%q status=%q",
			noMetrics.UtilizationFormatted, noMetrics.UtilizationStatus)
	}
	if noMetrics.SavingsFormatted != "" || noMetrics.SavingsPctFormatted != "" {
		t.Errorf("missing savings should be empty, got formatted=%q pct=%q",
			noMetrics.SavingsFormatted, noMetrics.SavingsPctFormatted)
	}
	if noMetrics.CoverageRingSVG != "" || noMetrics.UtilizationRingSVG != "" || noMetrics.SavingsDonutSVG != "" {
		t.Error("missing metrics should not render chart SVGs")
	}

	partial := view.AccountRows[1]
	if partial.CoverageStatus != "" || partial.CoverageFormatted != "" {
		t.Errorf("missing coverage on partial account should be empty, got formatted=%q status=%q",
			partial.CoverageFormatted, partial.CoverageStatus)
	}
	if partial.UtilizationFormatted != "88.0%" {
		t.Errorf("UtilizationFormatted = %q, want 88.0%%", partial.UtilizationFormatted)
	}
}

func TestPopulateDashboard_savingsBarWidthNonNegative(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{
			{
				AccountName:  "Positive savings",
				SavingsTotal: coresp.PeriodSavings{NetSavings: 1000, OnDemandCostEquivalent: 10000, OK: true},
			},
			{
				AccountName:  "Negative savings",
				SavingsTotal: coresp.PeriodSavings{NetSavings: -500, OnDemandCostEquivalent: 5000, OK: true},
			},
		},
	})
	if len(view.AccountRows) != 2 {
		t.Fatalf("AccountRows len = %d, want 2", len(view.AccountRows))
	}
	if view.AccountRows[0].SavingsBarWidthPct != 100 {
		t.Errorf("positive account SavingsBarWidthPct = %d, want 100", view.AccountRows[0].SavingsBarWidthPct)
	}
	if view.AccountRows[1].SavingsBarWidthPct != 0 {
		t.Errorf("negative account SavingsBarWidthPct = %d, want 0", view.AccountRows[1].SavingsBarWidthPct)
	}
}
