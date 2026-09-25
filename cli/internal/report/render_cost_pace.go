package report

import (
	"fmt"
	"html/template"
	"io"
	"math"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/report/costpace"
)

type costPaceView struct {
	Report                                                   costpace.Report
	AccountSummary, GeneratedAt, Month, AsOf                 string
	CurrentPeriod, PreviousPeriod                            string
	CurrentComparisonPeriod, PreviousComparisonPeriod        string
	CurrentMTD, DailyAverage, ProjectedTotal                 string
	CurrentComparable, PreviousComparable, PreviousFullMonth string
	PacePercent, ProjectedChangePercent, Direction           string
}

// RenderCostPaceHTML writes a standalone report using the shared HTML layout.
func RenderCostPaceHTML(w io.Writer, r costpace.Report, accountSummary string) error {
	tpl, err := template.ParseFS(templateFS, "templates/layout.html", "templates/cost-pace.html")
	if err != nil {
		return fmt.Errorf("parse cost-pace template: %w", err)
	}
	period := func(dr cost.DateRange) string {
		return cost.FormatDate(dr.Start) + " — " + cost.FormatDate(dr.End.AddDate(0, 0, -1))
	}
	direction := "Unavailable"
	if r.PacePercent != nil {
		switch {
		case math.Abs(*r.PacePercent) < 1e-9:
			direction = "Unchanged spend"
		case *r.PacePercent > 0:
			direction = "Higher spend"
		default:
			direction = "Lower spend"
		}
	}
	view := costPaceView{
		Report: r, AccountSummary: accountSummary,
		GeneratedAt:   r.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		Month:         r.CurrentPeriod.Start.Format("Jan 2006"),
		AsOf:          cost.FormatDate(r.CurrentPeriod.End.AddDate(0, 0, -1)),
		CurrentPeriod: period(r.CurrentPeriod), PreviousPeriod: period(r.PreviousPeriod),
		CurrentComparisonPeriod:  period(cost.DateRange{Start: r.CurrentPeriod.Start, End: r.CurrentPeriod.Start.AddDate(0, 0, r.ComparisonDays)}),
		PreviousComparisonPeriod: period(cost.DateRange{Start: r.PreviousPeriod.Start, End: r.PreviousPeriod.Start.AddDate(0, 0, r.ComparisonDays)}),
		CurrentMTD:               format.FormatMoney(r.CurrentMTD, r.Currency),
		DailyAverage:             format.FormatMoney(r.DailyAverage, r.Currency),
		ProjectedTotal:           format.FormatMoney(r.ProjectedTotal, r.Currency),
		CurrentComparable:        format.FormatMoney(r.CurrentComparable, r.Currency),
		PreviousComparable:       format.FormatMoney(r.PreviousComparable, r.Currency),
		PreviousFullMonth:        format.FormatMoney(r.PreviousFullMonth, r.Currency),
		PacePercent:              costPacePercent(r.PacePercent),
		ProjectedChangePercent:   costPacePercent(r.ProjectedChangePercent), Direction: direction,
	}
	return tpl.ExecuteTemplate(w, "cost-pace", view)
}

func costPacePercent(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%+.1f%%", *value)
}
