package report

import (
	"fmt"
	"html/template"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	coresp "github.com/openshift-online/finops-tools/core/report/savingsplans"
)

const savingsPlansCurrency = "USD"

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

// SavingsPlansSavingsView is one row in the savings table.
type SavingsPlansSavingsView struct {
	Month               string
	NetSavingsFormatted string
	PercentageFormatted string
}

// SavingsPlansSavingsTotal is AWS-reported net savings for the full period.
type SavingsPlansSavingsTotal struct {
	NetSavingsFormatted string
	PercentageFormatted string
}

// SavingsPlansAccountView is coverage, utilization, and savings for one account.
type SavingsPlansAccountView struct {
	AccountName        string
	IsLinked           bool
	Coverage           []SavingsPlansMetricView
	CoverageAverage    SavingsPlansMetricAverage
	Utilization        []SavingsPlansMetricView
	UtilizationAverage SavingsPlansMetricAverage
	Savings            []SavingsPlansSavingsView
	SavingsTotal       SavingsPlansSavingsTotal
}

// SavingsPlansDashboardAccountView is one row in the performance summary table.
type SavingsPlansDashboardAccountView struct {
	AccountName            string
	Color                  string
	CoverageFormatted      string
	CoverageRingSVG        template.HTML
	CoverageStatus         string
	CoverageStatusClass    string
	UtilizationFormatted   string
	UtilizationRingSVG     template.HTML
	UtilizationStatus      string
	UtilizationStatusClass string
	SavingsFormatted       string
	SavingsCompact         string
	SavingsBarWidthPct     int
	SavingsPctFormatted    string
	SavingsDonutSVG        template.HTML
}

// SavingsPlansReportView is the template context for savings-plans.html.
type SavingsPlansReportView struct {
	GeneratedAt    string
	AccountSummary string
	StartDate      string
	EndDate        string
	AccountCount   int

	TotalSavingsCompact      string
	TotalSavingsPctFormatted string
	AvgCoverageFormatted     string
	AvgCoverageStatusHTML    template.HTML
	AvgUtilizationFormatted  string
	AvgUtilizationStatusHTML template.HTML
	TotalOnDemandCompact     string

	BubbleChartSVG template.HTML
	AccountRows    []SavingsPlansDashboardAccountView
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
			Savings:            savingsToView(acct.Savings, r.StartDate, r.EndDate),
			SavingsTotal:       savingsTotalToView(acct.SavingsTotal),
		})
	}

	view := SavingsPlansReportView{
		GeneratedAt:    r.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		AccountSummary: strings.Join(names, ", "),
		StartDate:      r.StartDate,
		EndDate:        r.EndDate,
		AccountCount:   len(r.Accounts),
		Accounts:       accounts,
	}
	populateDashboard(&view, r.Accounts)
	return view
}

func populateDashboard(view *SavingsPlansReportView, accounts []coresp.AccountReport) {
	var (
		totalSavings     float64
		totalOnDemand    float64
		coverageSum      float64
		coverageWeight   float64
		coverageOnDemand float64
		coverageCount    int
		utilSum          float64
		utilCount        int
	)

	bubbles := make([]spBubblePoint, 0, len(accounts))
	dashboardRows := make([]SavingsPlansDashboardAccountView, 0, len(accounts))
	maxSavings := 0.0

	for i, acct := range accounts {
		color := spAccountColors[i%len(spAccountColors)]

		covPct := 0.0
		if acct.CoverageAverage.OK {
			covPct = acct.CoverageAverage.Percentage
			coverageSum += covPct
			coverageCount++
			if acct.SavingsTotal.OK && acct.SavingsTotal.OnDemandCostEquivalent > 0 {
				onDemand := acct.SavingsTotal.OnDemandCostEquivalent
				coverageWeight += covPct * onDemand
				coverageOnDemand += onDemand
			}
		}

		utilPct := 0.0
		if acct.UtilizationAverage.OK {
			utilPct = acct.UtilizationAverage.Percentage
			utilSum += utilPct
			utilCount++
		}

		savings := 0.0
		onDemand := 0.0
		savingsPct := 0.0
		if acct.SavingsTotal.OK {
			savings = acct.SavingsTotal.NetSavings
			onDemand = acct.SavingsTotal.OnDemandCostEquivalent
			savingsPct = acct.SavingsTotal.SavingsPercentage()
			totalSavings += savings
			totalOnDemand += onDemand
			if savings > maxSavings {
				maxSavings = savings
			}
		}

		row := SavingsPlansDashboardAccountView{
			AccountName: acct.AccountName,
			Color:       color,
		}
		if acct.CoverageAverage.OK {
			covStatus, covClass := dashboardCoverageStatus(covPct)
			row.CoverageFormatted = fmt.Sprintf("%.1f%%", covPct)
			row.CoverageRingSVG = template.HTML(spProgressRingSVG(covPct, covClass, 72))
			row.CoverageStatus = covStatus
			row.CoverageStatusClass = covClass
		}
		if acct.UtilizationAverage.OK {
			utilStatus, utilClass := dashboardUtilizationStatus(utilPct)
			row.UtilizationFormatted = fmt.Sprintf("%.1f%%", utilPct)
			row.UtilizationRingSVG = template.HTML(spProgressRingSVG(utilPct, utilClass, 72))
			row.UtilizationStatus = utilStatus
			row.UtilizationStatusClass = utilClass
		}
		if acct.SavingsTotal.OK {
			row.SavingsFormatted = format.FormatMoney(savings, savingsPlansCurrency)
			row.SavingsCompact = formatCompactUSD(savings)
			row.SavingsPctFormatted = fmt.Sprintf("%.1f%%", savingsPct)
			row.SavingsDonutSVG = template.HTML(spSavingsDonutSVG(savingsPct, color, 36))
		}
		dashboardRows = append(dashboardRows, row)

		if acct.CoverageAverage.OK && acct.UtilizationAverage.OK {
			bubbles = append(bubbles, spBubblePoint{
				Label:          acct.AccountName,
				Coverage:       covPct,
				Utilization:    utilPct,
				Savings:        savings,
				SavingsCompact: formatCompactUSD(savings),
				Color:          color,
			})
		}
	}

	for i := range dashboardRows {
		if maxSavings > 0 && accounts[i].SavingsTotal.OK {
			net := accounts[i].SavingsTotal.NetSavings
			pct := 0
			if net > 0 {
				pct = int(net / maxSavings * 100)
				if pct < 4 {
					pct = 4
				}
			}
			dashboardRows[i].SavingsBarWidthPct = pct
		}
	}

	view.AccountRows = dashboardRows
	view.BubbleChartSVG = template.HTML(spBubbleChartSVG(bubbles))

	if totalSavings > 0 || totalOnDemand > 0 {
		view.TotalSavingsCompact = formatCompactUSD(totalSavings)
		view.TotalOnDemandCompact = formatCompactUSD(totalOnDemand)
		if totalOnDemand > 0 {
			view.TotalSavingsPctFormatted = fmt.Sprintf("%.1f%%", totalSavings/totalOnDemand*100)
		}
	}

	avgCov := 0.0
	if coverageWeight > 0 && coverageOnDemand > 0 {
		avgCov = coverageWeight / coverageOnDemand
	} else if coverageCount > 0 {
		avgCov = coverageSum / float64(coverageCount)
	}
	if coverageCount > 0 {
		view.AvgCoverageFormatted = fmt.Sprintf("%.1f%%", avgCov)
		view.AvgCoverageStatusHTML = dashboardStatusHTML(avgCov, true)
	}

	avgUtil := 0.0
	if utilCount > 0 {
		avgUtil = utilSum / float64(utilCount)
		view.AvgUtilizationFormatted = fmt.Sprintf("%.1f%%", avgUtil)
		view.AvgUtilizationStatusHTML = dashboardStatusHTML(avgUtil, false)
	}
}

func dashboardCoverageStatus(pct float64) (label, class string) {
	switch {
	case pct >= 95:
		return "Good", "status-good"
	case pct >= 80:
		return "Watch", "status-watch"
	default:
		return "Poor", "status-poor"
	}
}

func dashboardUtilizationStatus(pct float64) (label, class string) {
	switch {
	case pct >= 95:
		return "Good", "status-good"
	case pct >= 70:
		return "Watch", "status-watch"
	default:
		return "Poor", "status-poor"
	}
}

func dashboardStatusHTML(pct float64, isCoverage bool) template.HTML {
	var label, class string
	if isCoverage {
		label, class = dashboardCoverageStatus(pct)
	} else {
		label, class = dashboardUtilizationStatus(pct)
	}
	color := spStatusColor(class)
	icon := "&#x2713;"
	switch class {
	case "status-watch":
		icon = "&#x26A0;"
	case "status-poor":
		icon = "&#x2717;"
	}
	return template.HTML(fmt.Sprintf(`<span style="color:%s">%s %s</span>`, color, icon, label))
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

func savingsToView(
	savings []coresp.MonthlySavings,
	rangeStart, rangeEnd string,
) []SavingsPlansSavingsView {
	rows := make([]SavingsPlansSavingsView, 0, len(savings))
	for _, s := range savings {
		rows = append(rows, SavingsPlansSavingsView{
			Month:               monthDisplayLabel(s.Month, rangeStart, rangeEnd),
			NetSavingsFormatted: format.FormatMoney(s.NetSavings, savingsPlansCurrency),
			PercentageFormatted: fmt.Sprintf("%.1f%%", s.SavingsPercentage()),
		})
	}
	return rows
}

func savingsTotalToView(total coresp.PeriodSavings) SavingsPlansSavingsTotal {
	if !total.OK {
		return SavingsPlansSavingsTotal{}
	}
	return SavingsPlansSavingsTotal{
		NetSavingsFormatted: format.FormatMoney(total.NetSavings, savingsPlansCurrency),
		PercentageFormatted: fmt.Sprintf("%.1f%%", total.SavingsPercentage()),
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
	return dashboardStatusHTML(pct, true)
}

func utilizationStatusHTML(pct float64) template.HTML {
	return dashboardStatusHTML(pct, false)
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
