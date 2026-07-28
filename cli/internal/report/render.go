package report

import (
	"fmt"
	"html/template"
	"io"
	"strings"
	"sync"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	corereport "github.com/openshift-online/finops-tools/core/report"
	"github.com/openshift-online/finops-tools/core/cost"
)

const costsTemplate = "costs"

var (
	initOnce sync.Once
	tpl      *template.Template
	initErr  error
)

func costsTemplateCompiled() (*template.Template, error) {
	initOnce.Do(func() {
		tpl, initErr = template.ParseFS(templateFS,
			"templates/layout.html",
			"templates/costs.html",
		)
	})
	return tpl, initErr
}

// CostsReportView is the template context for the costs HTML report.
type CostsReportView struct {
	GeneratedAt     string
	AccountSummary  string
	StartDate       string
	EndDate         string
	Currency        string
	Metric          string
	Total           float64
	TotalFormatted  string
	ByAccount       []BreakdownRowView
	ByService       []BreakdownRowView
	Daily           []cost.DailyCostItem
	DailyChartSVG   template.HTML
}

// BreakdownRowView is one breakdown table row for templates.
type BreakdownRowView struct {
	Label            string
	Amount           float64
	AmountFormatted  string
	Percent          float64
	PercentFormatted string
}

// NewCostsReportView maps a core CostsReport for HTML rendering.
func NewCostsReportView(r corereport.CostsReport) CostsReportView {
	view := CostsReportView{
		GeneratedAt:    r.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		AccountSummary: formatAccountSummary(r.Accounts),
		StartDate:      r.StartDate,
		EndDate:        r.EndDate,
		Currency:       r.Currency,
		Metric:         r.Metric,
		Total:          r.Total,
		TotalFormatted: format.FormatMoney(r.Total, r.Currency),
		ByAccount:      breakdownRows(r.ByAccount, cost.GroupByAccount, r.Total, r.Currency),
		ByService:      breakdownRows(r.ByService, cost.GroupByService, r.Total, r.Currency),
		Daily:          r.Daily,
	}
	view.DailyChartSVG = template.HTML(dailyChartSVG(view.Daily, view.Currency))
	return view
}

func breakdownRows(items []cost.CostBreakdownItem, groupBy cost.GroupBy, total float64, currency string) []BreakdownRowView {
	rows := make([]BreakdownRowView, 0, len(items))
	for _, item := range items {
		pct := corereport.PercentOfTotal(item.Amount, total)
		rows = append(rows, BreakdownRowView{
			Label:            item.DisplayLabel(groupBy),
			Amount:           item.Amount,
			AmountFormatted:  format.FormatMoney(item.Amount, currency),
			Percent:          pct,
			PercentFormatted: fmt.Sprintf("%.1f%%", pct),
		})
	}
	return rows
}

// FormatAccountSummary returns a comma-separated display string for a list of account targets.
func FormatAccountSummary(accounts []cost.AccountTarget) string {
	return formatAccountSummary(accounts)
}

func formatAccountSummary(accounts []cost.AccountTarget) string {
	if len(accounts) == 0 {
		return ""
	}
	names := make([]string, 0, len(accounts))
	for _, a := range accounts {
		if name := strings.TrimSpace(a.DisplayName); name != "" {
			names = append(names, name)
			continue
		}
		names = append(names, a.AccountID)
	}
	return strings.Join(names, ", ")
}

// RenderCostsHTML renders the costs report as HTML to w.
func RenderCostsHTML(w io.Writer, r corereport.CostsReport) error {
	t, err := costsTemplateCompiled()
	if err != nil {
		return fmt.Errorf("compile costs template: %w", err)
	}
	view := NewCostsReportView(r)
	if err := t.ExecuteTemplate(w, costsTemplate, view); err != nil {
		return fmt.Errorf("render costs template: %w", err)
	}
	return nil
}
