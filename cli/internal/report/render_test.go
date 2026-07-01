package report

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	corereport "github.com/openshift-online/finops-tools/core/report"
	coresp "github.com/openshift-online/finops-tools/core/report/savingsplans"
	"github.com/openshift-online/finops-tools/core/cost"
)

func TestRenderCostsHTML(t *testing.T) {
	var buf bytes.Buffer
	err := RenderCostsHTML(&buf, corereport.CostsReport{
		GeneratedAt: time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC),
		StartDate:   "2026-04-25",
		EndDate:     "2026-05-24",
		Currency:    "USD",
		Metric:      "NetAmortizedCost",
		Total:       1000,
		ByAccount: []cost.CostBreakdownItem{
			{Account: "111111111111", AccountName: "Member", Amount: 600},
		},
		ByService: []cost.CostBreakdownItem{
			{Service: "Amazon EC2", Amount: 700},
		},
		Daily: []cost.DailyCostItem{
			{Date: "2026-05-23", Amount: 30},
			{Date: "2026-05-24", Amount: 40},
		},
		Accounts: []cost.AccountTarget{{
			AccountID:   "123456789012",
			DisplayName: "RH Control Production",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Costs Report",
		"RH Control Production",
		"USD 1,000.00",
		"Member",
		"Amazon EC2",
		`<svg class="daily-chart"`,
		"2026-05-23",
		"2026-05-24",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRenderSavingsPlansHTML(t *testing.T) {
	// StartDate/EndDate use full calendar dates, matching core/report/savingsplans.Build().
	var buf bytes.Buffer
	err := RenderSavingsPlansHTML(&buf, coresp.Report{
		GeneratedAt: time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC),
		StartDate:   "2026-01-15",
		EndDate:     "2026-06-08",
		Accounts: []coresp.AccountReport{
			{
				AccountName: "RH Control Production",
				Coverage: []coresp.MonthlyMetric{
					{Month: "2026-01", Percentage: 85.0},
					{Month: "2026-02", Percentage: 65.0},
					{Month: "2026-03", Percentage: 70.0},
					{Month: "2026-06", Percentage: 78.0},
				},
				CoverageAverage: coresp.PeriodAverage{Percentage: 76.2, OK: true},
				Utilization: []coresp.MonthlyMetric{
					{Month: "2026-01", Percentage: 96.0},
					{Month: "2026-02", Percentage: 55.0},
					{Month: "2026-06", Percentage: 81.0},
				},
				UtilizationAverage: coresp.PeriodAverage{Percentage: 79.1, OK: true},
				Savings: []coresp.MonthlySavings{
					{Month: "2026-01", NetSavings: 10000, OnDemandCostEquivalent: 50000},
					{Month: "2026-02", NetSavings: 5000, OnDemandCostEquivalent: 25000},
				},
				SavingsTotal: coresp.PeriodSavings{NetSavings: 15000, OnDemandCostEquivalent: 75000, OK: true},
			},
			{
				AccountName: "Member One",
				Coverage: []coresp.MonthlyMetric{
					{Month: "2026-01", Percentage: 72.0},
				},
				CoverageAverage: coresp.PeriodAverage{Percentage: 72.0, OK: true},
				Utilization: []coresp.MonthlyMetric{
					{Month: "2026-01", Percentage: 88.0},
				},
				UtilizationAverage: coresp.PeriodAverage{Percentage: 88.0, OK: true},
				Savings: []coresp.MonthlySavings{
					{Month: "2026-01", NetSavings: 250, OnDemandCostEquivalent: 1000},
				},
				SavingsTotal: coresp.PeriodSavings{NetSavings: 250, OnDemandCostEquivalent: 1000, OK: true},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Savings Plans Report",
		"RH Control Production",
		"Member One",
		"Total Savings",
		"Avg Coverage",
		"Avg Utilization",
		"Coverage vs. Utilization",
		"Performance &amp; Savings by Account",
		"Account Detail",
		"Coverage",
		"Utilization",
		"Savings",
		"Period total",
		"USD 10,000.00",
		"USD 15,000.00",
		"USD 250.00",
		"20.0%",
		"25.0%",
		"2026-01 (from 15)",
		"2026-03",
		"2026-06 (through 8)",
		"85.0%",
		"65.0%",
		"96.0%",
		"55.0%",
		"72.0%",
		"88.0%",
		"Period average",
		`<svg class="sp-bubble-chart"`,
		`<svg class="sp-ring"`,
		"76.2%",
		"79.1%",
		"Watch",
		"Poor",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if !strings.Contains(out, "2026-01-15 — 2026-06-08") {
		t.Errorf("period line should use full dates from Build(); got excerpt around Period:\n%s", excerptAround(out, "2026-01-15"))
	}
	for _, want := range []string{"Good", "Watch", "Poor"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing detail status %q", want)
		}
	}
}

func TestNewSavingsPlansReportView_linkedOmitsStatus(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName: "Linked Member",
			IsLinked:    true,
			Coverage: []coresp.MonthlyMetric{
				{Month: "2026-01", Percentage: 50.0},
			},
		}},
	})
	if !view.Accounts[0].IsLinked {
		t.Fatal("expected linked account view")
	}
	if view.Accounts[0].Coverage[0].StatusHTML != "" {
		t.Errorf("linked coverage status = %q, want empty", view.Accounts[0].Coverage[0].StatusHTML)
	}
}

func TestNewSavingsPlansReportView_dashboard(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName:        "test",
			CoverageAverage:    coresp.PeriodAverage{Percentage: 71.5, OK: true},
			UtilizationAverage: coresp.PeriodAverage{Percentage: 82.0, OK: true},
			SavingsTotal:       coresp.PeriodSavings{NetSavings: 1234.5, OnDemandCostEquivalent: 5000, OK: true},
		}},
	})
	if len(view.AccountRows) != 1 {
		t.Fatalf("AccountRows len = %d, want 1", len(view.AccountRows))
	}
	row := view.AccountRows[0]
	if row.CoverageFormatted != "71.5%" {
		t.Errorf("coverage = %q", row.CoverageFormatted)
	}
	if row.SavingsFormatted != "USD 1,234.50" {
		t.Errorf("savings = %q", row.SavingsFormatted)
	}
	if row.CoverageStatus != "Poor" {
		t.Errorf("coverage status = %q, want Poor", row.CoverageStatus)
	}
}

func TestRenderSavingsPlansHTML_linkedOmitsStatusColumn(t *testing.T) {
	var buf bytes.Buffer
	err := RenderSavingsPlansHTML(&buf, coresp.Report{
		GeneratedAt: time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC),
		StartDate:   "2026-01-01",
		EndDate:     "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName: "Linked Member",
			IsLinked:    true,
			CoverageAverage: coresp.PeriodAverage{Percentage: 72.0, OK: true},
			UtilizationAverage: coresp.PeriodAverage{Percentage: 88.0, OK: true},
			Coverage: []coresp.MonthlyMetric{
				{Month: "2026-01", Percentage: 72.0},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Linked Member") {
		t.Fatal("output missing Linked Member")
	}
	if !strings.Contains(out, "Performance &amp; Savings by Account") {
		t.Error("expected dashboard performance table")
	}
	linkedSection := accountSectionHTML(out, "Linked Member")
	if linkedSection == "" {
		t.Fatal("output missing Linked Member account detail section")
	}
	if strings.Contains(linkedSection, "Good") || strings.Contains(linkedSection, "Watch") || strings.Contains(linkedSection, "Poor") {
		t.Errorf("linked account detail should not include status labels; got:\n%s", linkedSection)
	}
	if strings.Count(linkedSection, "<th>Status</th>") != 0 {
		t.Errorf("linked account detail should not render Status column header; got:\n%s", linkedSection)
	}
}

func TestRenderSavingsPlansHTML_savingsTotalWithoutMonthlyRows(t *testing.T) {
	var buf bytes.Buffer
	err := RenderSavingsPlansHTML(&buf, coresp.Report{
		GeneratedAt: time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC),
		StartDate:   "2026-01-01",
		EndDate:     "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName:  "Period Only",
			SavingsTotal: coresp.PeriodSavings{NetSavings: 5000, OnDemandCostEquivalent: 25000, OK: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	section := accountSectionHTML(out, "Period Only")
	if section == "" {
		t.Fatal("output missing Period Only account detail section")
	}
	for _, want := range []string{"Period total", "USD 5,000.00", "20.0%"} {
		if !strings.Contains(section, want) {
			t.Errorf("savings section missing %q; got:\n%s", want, section)
		}
	}
	if strings.Contains(section, "No savings data available for this period.") {
		t.Errorf("period total should render without monthly rows; got:\n%s", section)
	}
}

func TestMetricsToView_statusThresholds(t *testing.T) {
	coverage := metricsToView([]coresp.MonthlyMetric{
		{Month: "2026-01", Percentage: 85.0},
		{Month: "2026-02", Percentage: 65.0},
		{Month: "2026-03", Percentage: 50.0},
	}, "2026-01-01", "2026-03-31", coverageStatusHTML, true)
	utilization := metricsToView([]coresp.MonthlyMetric{
		{Month: "2026-01", Percentage: 92.0},
		{Month: "2026-02", Percentage: 75.0},
		{Month: "2026-03", Percentage: 55.0},
	}, "2026-01-01", "2026-03-31", utilizationStatusHTML, true)

	assertStatusLabel(t, coverage[0].StatusHTML, "Watch")
	assertStatusLabel(t, coverage[1].StatusHTML, "Poor")
	assertStatusLabel(t, coverage[2].StatusHTML, "Poor")
	assertStatusLabel(t, utilization[0].StatusHTML, "Watch")
	assertStatusLabel(t, utilization[1].StatusHTML, "Watch")
	assertStatusLabel(t, utilization[2].StatusHTML, "Poor")
}

func TestPeriodAverageToView(t *testing.T) {
	cov := periodAverageToView(coresp.PeriodAverage{Percentage: 71.5, OK: true}, coverageStatusHTML, true)
	util := periodAverageToView(coresp.PeriodAverage{Percentage: 82.0, OK: true}, utilizationStatusHTML, true)
	if cov.PercentageFormatted != "71.5%" {
		t.Errorf("coverage average = %q, want 71.5%%", cov.PercentageFormatted)
	}
	if util.PercentageFormatted != "82.0%" {
		t.Errorf("utilization average = %q, want 82.0%%", util.PercentageFormatted)
	}
	assertStatusLabel(t, cov.StatusHTML, "Poor")
	assertStatusLabel(t, util.StatusHTML, "Watch")
}

func TestSavingsToView(t *testing.T) {
	rows := savingsToView([]coresp.MonthlySavings{
		{Month: "2026-01", NetSavings: 1234.5, OnDemandCostEquivalent: 5000},
	}, "2026-01-01", "2026-03-31")
	if rows[0].NetSavingsFormatted != "USD 1,234.50" {
		t.Errorf("monthly savings = %q, want USD 1,234.50", rows[0].NetSavingsFormatted)
	}
	if rows[0].PercentageFormatted != "24.7%" {
		t.Errorf("monthly savings pct = %q, want 24.7%%", rows[0].PercentageFormatted)
	}
	total := savingsTotalToView(coresp.PeriodSavings{NetSavings: 1234.5, OnDemandCostEquivalent: 5000, OK: true})
	if total.NetSavingsFormatted != "USD 1,234.50" {
		t.Errorf("period savings = %q, want USD 1,234.50", total.NetSavingsFormatted)
	}
}

func TestNewSavingsPlansReportView_savingsEmpty(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName: "test",
		}},
	})
	if view.TotalSavingsCompact != "" {
		t.Errorf("empty savings total = %q, want empty", view.TotalSavingsCompact)
	}
}

func TestNewSavingsPlansReportView_periodAverageEmpty(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName: "test",
		}},
	})
	if view.AvgCoverageFormatted != "" {
		t.Errorf("empty coverage average = %q, want empty", view.AvgCoverageFormatted)
	}
	if view.AvgUtilizationFormatted != "" {
		t.Errorf("empty utilization average = %q, want empty", view.AvgUtilizationFormatted)
	}
}

func assertStatusLabel(t *testing.T, html template.HTML, want string) {
	t.Helper()
	if !strings.Contains(string(html), want) {
		t.Errorf("status HTML = %q, want label %q", html, want)
	}
}

func accountSectionHTML(html, accountName string) string {
	marker := "<h2>" + accountName + "</h2>"
	h2 := strings.Index(html, marker)
	if h2 < 0 {
		return ""
	}
	start := strings.LastIndex(html[:h2], `<section class="account-section">`)
	if start < 0 {
		return ""
	}
	segment := html[start:]
	depth := 0
	for i := 0; i < len(segment); {
		nextOpen := strings.Index(segment[i:], "<section")
		nextClose := strings.Index(segment[i:], "</section>")
		if nextClose < 0 {
			return segment
		}
		if nextOpen >= 0 && nextOpen < nextClose {
			depth++
			i += nextOpen + len("<section")
			continue
		}
		i += nextClose + len("</section>")
		depth--
		if depth == 0 {
			return segment[:i]
		}
	}
	return segment
}

func excerptAround(s, needle string) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return "(not found)"
	}
	end := i + 80
	if end > len(s) {
		end = len(s)
	}
	return s[i:end]
}

func TestFormatAccountSummary(t *testing.T) {
	s := formatAccountSummary([]cost.AccountTarget{{
		DisplayName: "Member Production",
		AccountID:   "111111111111",
	}})
	if s != "Member Production" {
		t.Errorf("got %q", s)
	}
}

func TestFormatAccountSummaryFallsBackToAccountID(t *testing.T) {
	s := formatAccountSummary([]cost.AccountTarget{{
		DisplayAlias: "quay",
		AccountID:    "111111111111",
	}})
	if s != "111111111111" {
		t.Errorf("got %q", s)
	}
}
