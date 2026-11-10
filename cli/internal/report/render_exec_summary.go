// render_exec_summary.go renders the executive summary report as HTML.
package report

import (
	"fmt"
	"html/template"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	"github.com/openshift-online/finops-tools/core/report/execsummary"
)

const execSummaryTemplate = "exec-summary"

var (
	execSummaryTplOnce sync.Once
	execSummaryTpl     *template.Template
	execSummaryTplErr  error
)

func execSummaryTemplateCompiled() (*template.Template, error) {
	execSummaryTplOnce.Do(func() {
		execSummaryTpl, execSummaryTplErr = template.ParseFS(templateFS,
			"templates/layout.html",
			"templates/exec-summary.html",
		)
	})
	return execSummaryTpl, execSummaryTplErr
}

// ExecSummaryMonthView is one row in the monthly totals table.
type ExecSummaryMonthView struct {
	Month        string
	Total        string
	DailyAverage string
	MoM          string
	MoMClass     string
	HasMoM       bool
	MoMDirection string
}

// ExecSummaryAccountServiceView is one (account, service, month) cost cell.
type ExecSummaryAccountServiceView struct {
	AccountName string
	AccountID   string
	IsPayer     bool
	Month       string
	Service     string
	Amount      string
}

// ExecSummaryAccountView is one account block with its per-month totals and services.
type ExecSummaryAccountView struct {
	AccountID   string
	AccountName string
	IsPayer     bool
	Total       string
	MonthTotals []ExecSummaryMonthTotalView
	Services    []ExecSummaryServiceView
}

// ExecSummaryMonthTotalView is an account's total for one month.
type ExecSummaryMonthTotalView struct {
	Month string
	Total string
}

// ExecSummaryServiceView is one service row inside an account block (latest-month costs).
type ExecSummaryServiceView struct {
	Service string
	Amount  string
}

// ExecSummaryReportView is the template context for exec-summary.html.
type ExecSummaryReportView struct {
	GeneratedAt    string
	PayerLabel     string
	PayerAccountID string
	AccountCount   int
	PeriodLabel    string
	Currency       string
	LatestMonth    string
	MonthlyTotal   string
	MoMLabel       string
	MoMClass       string
	MoMDirection   string
	Months         []ExecSummaryMonthView
	Accounts       []ExecSummaryAccountView
	ServiceRows    []ExecSummaryAccountServiceView
	// ZeroSpendNote is rendered when selected accounts had no spend over the
	// window, e.g. "12 account(s) with $0.00 in spend over the window".
	ZeroSpendNote string
}

// NewExecSummaryReportView maps a core execsummary.Report to the template context.
// Money stays numeric in the model; formatting happens only here.
func NewExecSummaryReportView(r execsummary.Report, generatedAt time.Time) ExecSummaryReportView {
	monthIndex := make(map[string]int, len(r.Months))
	for i, m := range r.Months {
		monthIndex[m.Month] = i
	}

	months := make([]ExecSummaryMonthView, 0, len(r.Months))
	for _, m := range r.Months {
		v := ExecSummaryMonthView{
			Month:        m.Month,
			Total:        format.FormatMoney(m.Total, r.Currency),
			DailyAverage: format.FormatMoney(m.DailyAverage, r.Currency),
		}
		if m.MoMPct != nil {
			v.HasMoM = true
			v.MoM = fmt.Sprintf("%+.1f%%", *m.MoMPct)
			v.MoMClass, v.MoMDirection = momClass(*m.MoMPct)
		} else {
			v.MoM = "N/A"
		}
		months = append(months, v)
	}

	latest := r.LatestMonth()
	view := ExecSummaryReportView{
		GeneratedAt:    generatedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		PayerLabel:     r.PayerAlias,
		PayerAccountID: r.PayerAccountID,
		AccountCount:   len(r.Accounts),
		PeriodLabel:    fmt.Sprintf("%s – %s", r.StartMonth, r.EndMonth),
		Currency:       r.Currency,
		LatestMonth:    latest.Month,
		MonthlyTotal:   format.FormatMoney(latest.Total, r.Currency),
		Months:         months,
	}
	if latest.MoMPct != nil {
		view.MoMLabel = fmt.Sprintf("%+.1f%%", *latest.MoMPct)
		view.MoMClass, view.MoMDirection = momClass(*latest.MoMPct)
	} else {
		view.MoMLabel = "N/A"
	}

	for _, a := range r.Accounts {
		av := ExecSummaryAccountView{
			AccountID:   a.AccountID,
			AccountName: a.DisplayName,
			IsPayer:     a.IsPayer,
			Total:       format.FormatMoney(a.Total, r.Currency),
		}
		for _, m := range r.Months {
			av.MonthTotals = append(av.MonthTotals, ExecSummaryMonthTotalView{
				Month: m.Month,
				Total: format.FormatMoney(a.MonthlyTotals[m.Month], r.Currency),
			})
		}
		// Latest-month services, highest cost first.
		type svc struct {
			service string
			amount  float64
		}
		var svcs []svc
		for _, s := range a.Services {
			if s.Month == latest.Month {
				svcs = append(svcs, svc{s.Service, s.Amount})
			}
		}
		sort.Slice(svcs, func(i, j int) bool { return svcs[i].amount > svcs[j].amount })
		for _, s := range svcs {
			av.Services = append(av.Services, ExecSummaryServiceView{
				Service: s.service,
				Amount:  format.FormatMoney(s.amount, r.Currency),
			})
		}
		view.Accounts = append(view.Accounts, av)
	}

	if r.ZeroSpendAccounts > 0 {
		view.ZeroSpendNote = fmt.Sprintf("%d account(s) with %s in spend over the window",
			r.ZeroSpendAccounts, format.FormatMoney(0, r.Currency))
	}

	// Full per-month account/service rows for the drill-down table.
	namesByID := make(map[string]string, len(r.Accounts))
	for _, a := range r.Accounts {
		namesByID[a.AccountID] = a.DisplayName
	}
	for _, row := range r.Rows {
		name, ok := namesByID[row.AccountID]
		if !ok {
			name = row.AccountID
		}
		view.ServiceRows = append(view.ServiceRows, ExecSummaryAccountServiceView{
			AccountName: name,
			AccountID:   row.AccountID,
			IsPayer:     row.AccountID == r.PayerAccountID,
			Month:       row.Month,
			Service:     row.Service,
			Amount:      format.FormatMoney(row.Amount, r.Currency),
		})
	}

	return view
}

func momClass(pct float64) (class, direction string) {
	switch {
	case pct > 0:
		return "mom-up", "▲"
	case pct < 0:
		return "mom-down", "▼"
	default:
		return "mom-flat", "—"
	}
}

// RenderExecSummaryHTML renders the executive summary report as HTML to w.
func RenderExecSummaryHTML(w io.Writer, r execsummary.Report) error {
	t, err := execSummaryTemplateCompiled()
	if err != nil {
		return fmt.Errorf("compile exec-summary template: %w", err)
	}
	view := NewExecSummaryReportView(r, time.Now().UTC())
	if err := t.ExecuteTemplate(w, execSummaryTemplate, view); err != nil {
		return fmt.Errorf("render exec-summary template: %w", err)
	}
	return nil
}
