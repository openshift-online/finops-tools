package report

import (
	"fmt"
	"sort"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

const (
	anomalyDisplayLimit  = 10
	alertBannerLimit     = 3
	alertBannerMinImpact = 10000.0
)

// buildGrowingAccountCards implements spec 7.3: one card per account,
// already sorted by delta descending by TopGrowingAccounts.
func buildGrowingAccountCards(accounts []execsum.GrowingAccountRecord) []GrowingAccountCardView {
	cards := make([]GrowingAccountCardView, 0, len(accounts))
	for _, a := range accounts {
		arrow := "▲"
		deltaClass := "badge-up"
		if a.Delta < 0 {
			arrow = "▼"
			deltaClass = "badge-down"
		}
		services := make([]ServiceCostRowView, 0, len(a.TopServices))
		for _, s := range a.TopServices {
			services = append(services, ServiceCostRowView{
				Service:       s.Service,
				CostFormatted: format.FormatDollarCompact(s.Cost),
			})
		}
		cards = append(cards, GrowingAccountCardView{
			AccountName:            a.AccountName,
			ServiceName:            a.ServiceName,
			LastMonthCostFormatted: format.FormatDollarCompact(a.LastMonthCost),
			DeltaFormatted:         fmt.Sprintf("%s %s", arrow, format.FormatDollarCompactShort(absFloat(a.Delta))),
			DeltaClass:             deltaClass,
			TopServices:            services,
		})
	}
	return cards
}

// execSummaryAnomalySeverity implements spec 7.4's severity thresholds,
// distinct from the standalone cost-anomalies report's thresholds
// (render_cost_anomalies.go's anomalySeverity uses $10k/$1k/$100 --
// deliberately left unchanged there to avoid altering that report's
// already-shipped output).
func execSummaryAnomalySeverity(impact float64) BadgeView {
	switch {
	case impact > 50000:
		return BadgeView{Label: "Critical", Class: "badge-critical"}
	case impact > 10000:
		return BadgeView{Label: "High", Class: "badge-high"}
	default:
		return BadgeView{Label: "Medium", Class: "badge-medium"}
	}
}

// payerDisplayLabel converts the internal lowercase payer tag (used
// throughout core/report/execsummary as a stable machine-readable join/group
// key, e.g. "production") into its human-readable label ("Production") for
// table display. Falls back to the raw tag when it doesn't match a known
// payer, so unexpected values still render instead of disappearing.
func payerDisplayLabel(tag string) string {
	for _, pd := range execSummaryPayerDefs {
		if string(pd.ID) == tag {
			return pd.Label
		}
	}
	return tag
}

// buildAnomalyRows implements spec 7.4: a single combined table of AWS ML
// anomalies (top 10 by total impact, descending) followed by statistical
// anomalies (top 10 of the spec 6.9-capped 15, already sorted by |z-score|
// descending).
func buildAnomalyRows(p execsum.PayerReport) ([]AnomalyRowView, bool) {
	ml := append([]execsum.MLAnomalyRecord(nil), p.MLAnomalies...)
	sort.Slice(ml, func(i, j int) bool { return ml[i].TotalImpact > ml[j].TotalImpact })
	if len(ml) > anomalyDisplayLimit {
		ml = ml[:anomalyDisplayLimit]
	}

	rows := make([]AnomalyRowView, 0, len(ml)+anomalyDisplayLimit)
	for _, a := range ml {
		rows = append(rows, AnomalyRowView{
			Payer:           payerDisplayLabel(a.Payer),
			Service:         a.Service,
			Month:           a.StartDate,
			ImpactFormatted: format.FormatDollarCompact(a.TotalImpact),
			Severity:        execSummaryAnomalySeverity(a.TotalImpact),
			Method:          BadgeView{Label: "AWS ML", Class: "badge-calc"},
		})
	}

	statistical := p.StatisticalAnomalies
	if len(statistical) > anomalyDisplayLimit {
		statistical = statistical[:anomalyDisplayLimit]
	}
	for _, a := range statistical {
		direction := "+"
		directionClass := "kpi-bad"
		if a.Direction == "drop" {
			direction = "-"
			directionClass = "kpi-good"
		}
		rows = append(rows, AnomalyRowView{
			Payer:           payerDisplayLabel(a.Payer),
			Service:         a.ServiceName,
			Month:           a.Month,
			ImpactFormatted: format.FormatDollarCompact(absFloat(a.CurrentCost - a.MeanCost)),
			Severity:        execSummaryAnomalySeverity(absFloat(a.CurrentCost - a.MeanCost)),
			Method:          BadgeView{Label: "Statistical", Class: "badge-calc"},
			Direction:       direction,
			DirectionClass:  directionClass,
			ExtraDetail:     fmt.Sprintf("Z=%+.1f · %+.1f%%", a.ZScore, a.PctChange),
		})
	}

	return rows, len(rows) > 0
}

// buildAlertBanner implements spec 7.4's All-Payers-tab alert banner: up to
// 3 AWS ML anomalies with impact > $10,000, formatted as one line each.
func buildAlertBanner(ml []execsum.MLAnomalyRecord) []string {
	high := make([]execsum.MLAnomalyRecord, 0, len(ml))
	for _, a := range ml {
		if a.TotalImpact > alertBannerMinImpact {
			high = append(high, a)
		}
	}
	sort.Slice(high, func(i, j int) bool { return high[i].TotalImpact > high[j].TotalImpact })
	if len(high) > alertBannerLimit {
		high = high[:alertBannerLimit]
	}
	lines := make([]string, 0, len(high))
	for _, a := range high {
		lines = append(lines, fmt.Sprintf("%s — %s impact (%s)", a.Service, format.FormatDollarCompact(a.TotalImpact), a.Payer))
	}
	return lines
}
