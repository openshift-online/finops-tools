package report

import (
	"fmt"
	"sort"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

// buildCategorySection implements spec 7.2: a stacked-bar Chart.js spec
// (highest-total-cost category on top of the stack) and a table sorted by
// absolute month-over-month delta, descending, with rank badges on the top
// 3 rows whose delta is positive.
func buildCategorySection(p execsum.PayerReport, months []string) (chartSpec, []CategoryTableRowView) {
	order := p.CategoryOrder // already sorted by total cost, descending (spec 7.2)
	colorByCategory := make(map[string]string, len(order))
	for i, c := range order {
		colorByCategory[c] = paletteColor(i)
	}

	costByKey := make(map[[2]string]float64, len(p.CategoryMonths))
	for _, r := range p.CategoryMonths {
		costByKey[[2]string{r.ServiceName, r.Month}] = r.Cost
	}

	lastMonth, prevMonth := lastTwoMonths(months)

	rows := make([]CategoryTableRowView, 0, len(order))
	for _, category := range order {
		monthCosts := make([]string, len(months))
		for i, m := range months {
			monthCosts[i] = format.FormatDollarCompact(costByKey[[2]string{category, m}])
		}
		delta := costByKey[[2]string{category, lastMonth}] - costByKey[[2]string{category, prevMonth}]
		deltaClass := ""
		deltaFormatted := format.FormatDollarCompact(delta)
		switch {
		case delta > 0:
			deltaClass = "kpi-bad"
			deltaFormatted = "+" + deltaFormatted
		case delta < 0:
			deltaClass = "kpi-good"
		}
		rows = append(rows, CategoryTableRowView{
			ServiceName:    category,
			Color:          colorByCategory[category],
			MonthCosts:     monthCosts,
			DeltaFormatted: deltaFormatted,
			DeltaClass:     deltaClass,
		})
	}

	// Sort by absolute delta descending; recover the raw delta from the
	// formatted string isn't reliable, so sort using a parallel slice of
	// raw deltas computed the same way.
	type rowWithDelta struct {
		row   CategoryTableRowView
		delta float64
	}
	withDelta := make([]rowWithDelta, len(rows))
	for i, category := range order {
		delta := costByKey[[2]string{category, lastMonth}] - costByKey[[2]string{category, prevMonth}]
		withDelta[i] = rowWithDelta{row: rows[i], delta: delta}
	}
	sort.SliceStable(withDelta, func(i, j int) bool {
		return absFloat(withDelta[i].delta) > absFloat(withDelta[j].delta)
	})

	rank := 0
	for i := range withDelta {
		if withDelta[i].delta > 0 && rank < 3 {
			rank++
			withDelta[i].row.Rank = fmt.Sprintf("#%d", rank)
		}
	}

	sortedRows := make([]CategoryTableRowView, len(withDelta))
	for i, rd := range withDelta {
		sortedRows[i] = rd.row
	}

	// Chart datasets: Chart.js stacks bars in dataset-array order with the
	// first dataset at the bottom, so reverse the cost-descending order to
	// put the highest-total-cost category on top (spec 7.2).
	datasets := make([]chartDataset, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		category := order[i]
		data := make([]*float64, len(months))
		for j, m := range months {
			data[j] = floatPtr(costByKey[[2]string{category, m}])
		}
		datasets = append(datasets, chartDataset{
			Label: category,
			Data:  data,
			Color: colorByCategory[category],
		})
	}

	chart := chartSpec{
		CanvasID:    "category-chart-" + string(p.ID),
		Type:        "bar",
		Stacked:     true,
		Labels:      months,
		Datasets:    datasets,
		YTickFormat: "dollarK",
	}

	return chart, sortedRows
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
