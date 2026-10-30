package report

import (
	"fmt"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

// buildClusterGrowthTabView implements spec section 7.7: one KPI card per
// present product type, a cumulative-count trend line chart (with
// show/hide toggles per product), and a net-new-per-month stacked delta
// bar chart.
func buildClusterGrowthTabView(r execsum.ClusterGrowthTabReport) (ClusterGrowthTabView, []chartSpec) {
	months := r.Months

	type key struct{ product, month string }
	pointByKey := make(map[key]execsum.ClusterGrowthProductPoint, len(r.Points))
	productSet := make(map[string]bool)
	for _, p := range r.Points {
		pointByKey[key{p.ProductType, p.Month}] = p
		productSet[p.ProductType] = true
	}
	products := make([]string, 0, len(productSet))
	for p := range productSet {
		products = append(products, p)
	}
	products = execsum.SortProductTypes(products)

	lastMonth, prevMonth := lastTwoMonths(months)

	cards := make([]ClusterGrowthProductCardView, 0, len(products))
	trendDatasets := make([]chartDataset, 0, len(products))
	deltaDatasets := make([]chartDataset, 0, len(products))

	for i, product := range products {
		color := productColor(product, i)

		lastPoint := pointByKey[key{product, lastMonth}]
		prevPoint, hasPrev := pointByKey[key{product, prevMonth}]
		deltaFormatted := "—"
		deltaClass := ""
		if hasPrev {
			delta := lastPoint.Cumulative - prevPoint.Cumulative
			deltaFormatted = fmt.Sprintf("%+d", delta)
			if delta >= 0 {
				deltaClass = "kpi-good"
			} else {
				deltaClass = "kpi-warn"
			}
		}
		cards = append(cards, ClusterGrowthProductCardView{
			ProductType:         product,
			CumulativeFormatted: format.FormatAmountWhole(float64(lastPoint.Cumulative)),
			DeltaFormatted:      deltaFormatted,
			DeltaClass:          deltaClass,
			Color:               color,
		})

		trendData := make([]*float64, len(months))
		deltaData := make([]*float64, len(months))
		for j, m := range months {
			if pt, ok := pointByKey[key{product, m}]; ok {
				trendData[j] = floatPtr(float64(pt.Cumulative))
				deltaData[j] = floatPtr(float64(pt.NewClusters))
			}
		}
		trendDatasets = append(trendDatasets, chartDataset{Label: product, Data: trendData, Color: color})
		deltaDatasets = append(deltaDatasets, chartDataset{Label: product, Data: deltaData, Color: color})
	}

	trendChart := chartSpec{
		CanvasID:    "cluster-growth-trend-chart",
		Type:        "line",
		Labels:      months,
		Datasets:    trendDatasets,
		SpanGaps:    true,
		PointRadius: intPtr(4),
		Toggleable:  true,
	}
	deltaChart := chartSpec{
		CanvasID:    "cluster-growth-delta-chart",
		Type:        "bar",
		Stacked:     true,
		Labels:      months,
		Datasets:    deltaDatasets,
		YTickFormat: "signed",
	}

	tab := ClusterGrowthTabView{
		ProductCards: cards,
		TrendChart:   trendChart,
		DeltaChart:   deltaChart,
	}
	return tab, []chartSpec{trendChart, deltaChart}
}
