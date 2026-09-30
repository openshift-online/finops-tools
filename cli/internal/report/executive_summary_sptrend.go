package report

import (
	"github.com/openshift-online/finops-tools/cli/internal/format"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

// buildSPTrendView implements spec 7.5: a coverage/utilization line chart
// and a net-savings bar chart, plus the window-total net savings header
// line. Only called when p.HasSPData is true.
func buildSPTrendView(p execsum.PayerReport) (SPTrendView, []chartSpec) {
	months := make([]string, len(p.SPMonths))
	coverage := make([]*float64, len(p.SPMonths))
	utilization := make([]*float64, len(p.SPMonths))
	savings := make([]*float64, len(p.SPMonths))
	for i, m := range p.SPMonths {
		months[i] = m.Month
		coverage[i] = m.CoveragePercentage
		utilization[i] = m.UtilizationPercentage
		savings[i] = floatPtr(m.NetSavings)
	}

	covUtilChart := chartSpec{
		CanvasID: "sp-covutil-chart-" + string(p.ID),
		Type:     "line",
		Labels:   months,
		Datasets: []chartDataset{
			{Label: "SP Coverage %", Data: coverage, Color: "#0066cc"},
			{Label: "SP Utilization %", Data: utilization, Color: "#37a3a3"},
		},
		YMin:        floatPtr(0),
		YMax:        floatPtr(100),
		YTickFormat: "percent",
	}

	savingsChart := chartSpec{
		CanvasID: "sp-savings-chart-" + string(p.ID),
		Type:     "bar",
		Labels:   months,
		Datasets: []chartDataset{
			{Label: "Net Savings", Data: savings, Color: "#5e40be"},
		},
		YTickFormat: "dollarK",
	}

	view := SPTrendView{
		TotalNetSavingsFormatted: format.FormatDollarCompact(p.SPNetSavingsWindow),
		CoverageUtilChart:        covUtilChart,
		NetSavingsChart:          savingsChart,
	}
	return view, []chartSpec{covUtilChart, savingsChart}
}
