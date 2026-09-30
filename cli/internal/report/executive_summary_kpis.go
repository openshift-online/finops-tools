package report

import (
	"fmt"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

// kpiClass maps the execsummary package's "good"/"warn"/"bad" color-class
// strings (spec 6.10) to this template's CSS class names (spec 8.1).
func kpiClass(class string) string {
	switch class {
	case "good":
		return "kpi-good"
	case "warn":
		return "kpi-warn"
	case "bad":
		return "kpi-bad"
	default:
		return ""
	}
}

// buildPayerKPICards implements spec 7.1: the 9 KPI cards for one payer
// tab, in exact order, omitting the 4 SP-related cards when hasSPData is
// false.
func buildPayerKPICards(k execsum.PayerKPIs, hasSPData bool, lastMonthName string) []KPICardView {
	cards := []KPICardView{
		{
			Label:   "Total Spend",
			Value:   format.FormatDollarCompact(k.TotalCostLastMonth),
			SubText: lastMonthName,
		},
		{
			Label:   "MoM Change",
			Value:   fmt.Sprintf("%+.1f%%", k.MoMPct),
			SubText: fmt.Sprintf("vs prior month (%s)", format.FormatDollarCompactShort(k.TotalCostLastMonth-k.TotalCostPrevMonth)),
			Class:   kpiClass(k.MoMClass),
		},
	}

	if hasSPData {
		cards = append(cards,
			KPICardView{
				Label:   "SP Coverage",
				Value:   percentOrDash(k.SPCoverageAvg),
				SubText: "on-demand eligible",
				Class:   kpiClass(k.SPCoverageClass),
			},
			KPICardView{
				Label:   "SP Utilization",
				Value:   percentOrDash(k.SPUtilizationAvg),
				SubText: "committed SP used",
				Class:   kpiClass(k.SPUtilizationClass),
			},
			KPICardView{
				Label:   "SP Net Savings YTD",
				Value:   format.FormatDollarCompact(k.TotalNetSavings),
				SubText: "vs on-demand price",
				Class:   "kpi-purple",
			},
			KPICardView{
				Label:   "Savings Rate",
				Value:   percentOrDash(k.SavingsRate),
				SubText: "net savings / on-demand",
				Class:   kpiClass(k.SavingsRateClass),
			},
		)
	}

	clusterSub := "no prior snapshot"
	clusterClass := ""
	if k.ClusterDelta != nil {
		clusterSub = fmt.Sprintf("MoM delta: %+d", *k.ClusterDelta)
		if *k.ClusterDelta >= 0 {
			clusterClass = "kpi-good"
		} else {
			clusterClass = "kpi-warn"
		}
	}
	cards = append(cards, KPICardView{
		Label:   "Cluster Count",
		Value:   format.FormatAmountWhole(float64(k.ClusterCount)),
		SubText: clusterSub,
		Class:   clusterClass,
	})

	hcpUnitCost := "—"
	if k.HCPUnitCost != nil {
		hcpUnitCost = fmt.Sprintf("$%.4f", *k.HCPUnitCost)
	}
	cards = append(cards, KPICardView{
		Label:   "HCP Unit Cost",
		Value:   hcpUnitCost,
		SubText: "MC cost per cluster",
		Class:   "kpi-blue",
	})

	cards = append(cards, KPICardView{
		Label:   "Cost Anomalies",
		Value:   format.FormatAmountWhole(float64(k.AnomalyCount)),
		SubText: "detected spikes / drops",
		Class:   kpiClass(k.AnomalyClass),
	})

	return cards
}

// percentOrDash formats a *float64 percentage to one decimal place, or
// "—" if nil (spec 7.1's % formatting rule).
func percentOrDash(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", *v)
}
