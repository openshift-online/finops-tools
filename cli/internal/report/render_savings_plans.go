package report

import (
	"fmt"
	"html/template"
	"io"
	"strings"
	"sync"
	"time"

	coresp "github.com/openshift-online/finops-tools/core/report/savingsplans"
)

const savingsPlansTemplate = "savings-plans"

var (
	spTplOnce sync.Once
	spTpl     *template.Template
	spTplErr  error
)

func savingsPlansTemplateCompiled() (*template.Template, error) {
	spTplOnce.Do(func() {
		spTpl, spTplErr = template.ParseFS(templateFS,
			"templates/layout.html",
			"templates/savings-plans.html",
		)
	})
	return spTpl, spTplErr
}

// SavingsPlansMetricView is one row in the coverage or utilization table.
type SavingsPlansMetricView struct {
	Month               string
	Percentage          float64
	PercentageFormatted string
	StatusHTML          template.HTML
}

// SavingsPlansMetricAverage is AWS-reported coverage or utilization for the full period.
type SavingsPlansMetricAverage struct {
	PercentageFormatted string
	StatusHTML          template.HTML
}

// SavingsPlansAccountView is coverage and utilization for one account.
type SavingsPlansAccountView struct {
	AccountName         string
	IsLinked            bool
	Coverage            []SavingsPlansMetricView
	CoverageAverage     SavingsPlansMetricAverage
	Utilization         []SavingsPlansMetricView
	UtilizationAverage  SavingsPlansMetricAverage
}

// SavingsPlansReportView is the template context for savings-plans.html.
type SavingsPlansReportView struct {
	GeneratedAt    string
	AccountSummary string
	StartDate      string
	EndDate        string
	Accounts       []SavingsPlansAccountView
}

// NewSavingsPlansReportView maps a core savingsplans.Report to the template context.
func NewSavingsPlansReportView(r coresp.Report) SavingsPlansReportView {
	accounts := make([]SavingsPlansAccountView, 0, len(r.Accounts))
	names := make([]string, 0, len(r.Accounts))
	for _, acct := range r.Accounts {
		names = append(names, acct.AccountName)
		showStatus := !acct.IsLinked
		accounts = append(accounts, SavingsPlansAccountView{
			AccountName:        acct.AccountName,
			IsLinked:           acct.IsLinked,
			Coverage:           metricsToView(acct.Coverage, r.StartDate, r.EndDate, coverageStatusHTML, showStatus),
			CoverageAverage:    periodAverageToView(acct.CoverageAverage, coverageStatusHTML, showStatus),
			Utilization:        metricsToView(acct.Utilization, r.StartDate, r.EndDate, utilizationStatusHTML, showStatus),
			UtilizationAverage: periodAverageToView(acct.UtilizationAverage, utilizationStatusHTML, showStatus),
		})
	}
	return SavingsPlansReportView{
		GeneratedAt:    r.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		AccountSummary: strings.Join(names, ", "),
		StartDate:      r.StartDate,
		EndDate:        r.EndDate,
		Accounts:       accounts,
	}
}

func metricsToView(
	metrics []coresp.MonthlyMetric,
	rangeStart, rangeEnd string,
	statusFn func(float64) template.HTML,
	showStatus bool,
) []SavingsPlansMetricView {
	rows := make([]SavingsPlansMetricView, 0, len(metrics))
	for _, m := range metrics {
		var status template.HTML
		if showStatus {
			status = statusFn(m.Percentage)
		}
		rows = append(rows, SavingsPlansMetricView{
			Month:               monthDisplayLabel(m.Month, rangeStart, rangeEnd),
			Percentage:          m.Percentage,
			PercentageFormatted: fmt.Sprintf("%.1f%%", m.Percentage),
			StatusHTML:          status,
		})
	}
	return rows
}

func periodAverageToView(
	avg coresp.PeriodAverage,
	statusFn func(float64) template.HTML,
	showStatus bool,
) SavingsPlansMetricAverage {
	if !avg.OK {
		return SavingsPlansMetricAverage{}
	}
	var status template.HTML
	if showStatus {
		status = statusFn(avg.Percentage)
	}
	return SavingsPlansMetricAverage{
		PercentageFormatted: fmt.Sprintf("%.1f%%", avg.Percentage),
		StatusHTML:          status,
	}
}

// monthDisplayLabel annotates YYYY-MM when the report period only covers part of that month.
func monthDisplayLabel(month, rangeStart, rangeEnd string) string {
	start, err := time.ParseInLocation("2006-01-02", rangeStart, time.UTC)
	if err != nil {
		return month
	}
	end, err := time.ParseInLocation("2006-01-02", rangeEnd, time.UTC)
	if err != nil {
		return month
	}
	if month != start.Format("2006-01") && month != end.Format("2006-01") {
		return month
	}

	monthStart, err := time.ParseInLocation("2006-01", month, time.UTC)
	if err != nil {
		return month
	}
	lastDay := monthStart.AddDate(0, 1, -1).Day()

	startPartial := month == start.Format("2006-01") && start.Day() != 1
	endPartial := month == end.Format("2006-01") && end.Day() != lastDay

	switch {
	case startPartial && endPartial:
		return fmt.Sprintf("%s (%d – %d)", month, start.Day(), end.Day())
	case startPartial:
		return fmt.Sprintf("%s (from %d)", month, start.Day())
	case endPartial:
		return fmt.Sprintf("%s (through %d)", month, end.Day())
	default:
		return month
	}
}

func coverageStatusHTML(pct float64) template.HTML {
	switch {
	case pct >= 80:
		return `<span style="color:#22c55e">&#x2713; Good</span>`
	case pct >= 60:
		return `<span style="color:#f59e0b">&#x26A0; Low</span>`
	default:
		return `<span style="color:#ef4444">&#x2717; Critical</span>`
	}
}

func utilizationStatusHTML(pct float64) template.HTML {
	switch {
	case pct >= 90:
		return `<span style="color:#22c55e">&#x2713; Good</span>`
	case pct >= 70:
		return `<span style="color:#f59e0b">&#x26A0; Low</span>`
	default:
		return `<span style="color:#ef4444">&#x2717; Critical</span>`
	}
}

// RenderSavingsPlansHTML renders the savings plans report as HTML to w.
func RenderSavingsPlansHTML(w io.Writer, r coresp.Report) error {
	t, err := savingsPlansTemplateCompiled()
	if err != nil {
		return fmt.Errorf("compile savings-plans template: %w", err)
	}
	view := NewSavingsPlansReportView(r)
	if err := t.ExecuteTemplate(w, savingsPlansTemplate, view); err != nil {
		return fmt.Errorf("render savings-plans template: %w", err)
	}
	return nil
}
