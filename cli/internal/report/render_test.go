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
					{Month: "2026-01", Percentage: 92.0},
					{Month: "2026-02", Percentage: 55.0},
					{Month: "2026-06", Percentage: 81.0},
				},
				UtilizationAverage: coresp.PeriodAverage{Percentage: 79.1, OK: true},
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
		"Coverage",
		"Utilization",
		"2026-01 (from 15)",
		"2026-03",
		"2026-06 (through 8)",
		"85.0%",
		"65.0%",
		"92.0%",
		"55.0%",
		"72.0%",
		"88.0%",
		"Period average",
		"76.2%",
		"79.1%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if !strings.Contains(out, "<strong>Period:</strong> 2026-01-15 — 2026-06-08") {
		t.Errorf("period line should use full dates from Build(); got excerpt around Period:\n%s", excerptAround(out, "Period:"))
	}
	for _, want := range []string{"Good", "Low", "Critical"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing status %q", want)
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

func TestRenderSavingsPlansHTML_linkedOmitsStatusColumn(t *testing.T) {
	var buf bytes.Buffer
	err := RenderSavingsPlansHTML(&buf, coresp.Report{
		GeneratedAt: time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC),
		StartDate:   "2026-01-01",
		EndDate:     "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName: "Linked Member",
			IsLinked:    true,
			Coverage: []coresp.MonthlyMetric{
				{Month: "2026-01", Percentage: 72.0},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	linkedSection := accountSectionHTML(out, "Linked Member")
	if linkedSection == "" {
		t.Fatal("output missing Linked Member account section")
	}
	if strings.Contains(linkedSection, "Critical") || strings.Contains(linkedSection, "Good") || strings.Contains(linkedSection, "Low") {
		t.Errorf("linked account section should not include status labels; got:\n%s", linkedSection)
	}
	if strings.Count(linkedSection, "<th>Status</th>") != 0 {
		t.Errorf("linked account should not render Status column header; got:\n%s", linkedSection)
	}
}

func TestNewSavingsPlansReportView_statusThresholds(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName: "test",
			Coverage: []coresp.MonthlyMetric{
				{Month: "2026-01", Percentage: 85.0},
				{Month: "2026-02", Percentage: 65.0},
				{Month: "2026-03", Percentage: 50.0},
			},
			Utilization: []coresp.MonthlyMetric{
				{Month: "2026-01", Percentage: 92.0},
				{Month: "2026-02", Percentage: 75.0},
				{Month: "2026-03", Percentage: 55.0},
			},
		}},
	})

	acct := view.Accounts[0]
	assertStatusLabel(t, acct.Coverage[0].StatusHTML, "Good")
	assertStatusLabel(t, acct.Coverage[1].StatusHTML, "Low")
	assertStatusLabel(t, acct.Coverage[2].StatusHTML, "Critical")
	assertStatusLabel(t, acct.Utilization[0].StatusHTML, "Good")
	assertStatusLabel(t, acct.Utilization[1].StatusHTML, "Low")
	assertStatusLabel(t, acct.Utilization[2].StatusHTML, "Critical")
}

func TestNewSavingsPlansReportView_periodAverage(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName: "test",
			Coverage: []coresp.MonthlyMetric{
				{Month: "2026-01", Percentage: 80.0},
				{Month: "2026-02", Percentage: 60.0},
			},
			CoverageAverage: coresp.PeriodAverage{Percentage: 71.5, OK: true},
			Utilization: []coresp.MonthlyMetric{
				{Month: "2026-01", Percentage: 90.0},
				{Month: "2026-02", Percentage: 70.0},
			},
			UtilizationAverage: coresp.PeriodAverage{Percentage: 82.0, OK: true},
		}},
	})

	acct := view.Accounts[0]
	if acct.CoverageAverage.PercentageFormatted != "71.5%" {
		t.Errorf("coverage average = %q, want 71.5%%", acct.CoverageAverage.PercentageFormatted)
	}
	if acct.UtilizationAverage.PercentageFormatted != "82.0%" {
		t.Errorf("utilization average = %q, want 82.0%%", acct.UtilizationAverage.PercentageFormatted)
	}
	assertStatusLabel(t, acct.CoverageAverage.StatusHTML, "Low")
	assertStatusLabel(t, acct.UtilizationAverage.StatusHTML, "Low")
}

func TestNewSavingsPlansReportView_periodAverageEmpty(t *testing.T) {
	view := NewSavingsPlansReportView(coresp.Report{
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		Accounts: []coresp.AccountReport{{
			AccountName: "test",
		}},
	})
	acct := view.Accounts[0]
	if acct.CoverageAverage.PercentageFormatted != "" {
		t.Errorf("empty coverage average = %q, want empty", acct.CoverageAverage.PercentageFormatted)
	}
	if acct.UtilizationAverage.PercentageFormatted != "" {
		t.Errorf("empty utilization average = %q, want empty", acct.UtilizationAverage.PercentageFormatted)
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
