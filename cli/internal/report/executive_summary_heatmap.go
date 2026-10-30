package report

import (
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

const (
	mcSummaryTableLimit     = 25
	clusterDetailTableLimit = 100
	heatmapSparklineMonths  = 5
)

// heatmapWhite, heatmapGreen, and heatmapRed are the spec 8.4 color-formula
// endpoints (white -> green for positive values, white -> red for negative).
var (
	heatmapWhite = [3]int{255, 255, 255}
	heatmapGreen = [3]int{99, 153, 61}
	heatmapRed   = [3]int{238, 0, 0}
)

// lerpRGB implements spec 8.4's lerp_rgb: for each channel, round(c0 +
// ratio*(c1-c0)).
func lerpRGB(ratio float64, c0, c1 [3]int) string {
	ch := func(a, b int) int {
		return a + int(math.Round(ratio*float64(b-a)))
	}
	return fmt.Sprintf("#%02x%02x%02x", ch(c0[0], c1[0]), ch(c0[1], c1[1]), ch(c0[2], c1[2]))
}

// heatmapCellColor implements spec 8.4's asymmetric color formula: each
// side (positive/negative) is scaled independently by its own max, so both
// the most-profitable and the most-lossy region reach full saturation on
// their own side.
func heatmapCellColor(value, maxPos, maxNeg float64) (bg, text string) {
	var ratio float64
	if value >= 0 {
		if maxPos > 0 {
			ratio = math.Min(1.0, value/maxPos)
		}
		bg = lerpRGB(ratio, heatmapWhite, heatmapGreen)
	} else {
		if maxNeg > 0 {
			ratio = math.Min(1.0, -value/maxNeg)
		}
		bg = lerpRGB(ratio, heatmapWhite, heatmapRed)
	}
	if ratio > 0.55 {
		text = "#ffffff"
	} else {
		text = "#1a1a1a"
	}
	return bg, text
}

// sparklineSVG renders a 60x20 inline sparkline of values (spec 8.4's
// region row-header sparkline): min/max-normalized, 2px vertical margin,
// evenly spaced x positions, a solid dot at the last point, and a flat
// mid-height line when every value is equal.
func sparklineSVG(values []float64) template.HTML {
	const w, h, margin = 60.0, 20.0, 2.0
	if len(values) == 0 {
		return ""
	}
	minV, maxV := values[0], values[0]
	for _, v := range values {
		minV = math.Min(minV, v)
		maxV = math.Max(maxV, v)
	}
	n := len(values)
	xStep := w
	if n > 1 {
		xStep = w / float64(n-1)
	}

	points := make([]string, n)
	var lastX, lastY float64
	for i, v := range values {
		x := float64(i) * xStep
		if n == 1 {
			x = w / 2
		}
		y := h / 2
		if maxV != minV {
			frac := (v - minV) / (maxV - minV)
			y = h - margin - frac*(h-2*margin)
		}
		points[i] = fmt.Sprintf("%.1f,%.1f", x, y)
		lastX, lastY = x, y
	}

	return template.HTML(fmt.Sprintf(
		`<svg width="%d" height="%d" viewBox="0 0 %d %d" class="sparkline"><polyline points="%s" fill="none" stroke="#37a3a3" stroke-opacity="0.6" stroke-width="1.5"/><circle cx="%.1f" cy="%.1f" r="2" fill="#37a3a3"/></svg>`,
		int(w), int(h), int(w), int(h), strings.Join(points, " "), lastX, lastY,
	))
}

// momArrow returns the spec 7.6.2 MoM arrow ("↑"/"↓"/"→") and its color
// class: goodIfUp true means an increase is colored good (green); false
// means a decrease is colored good (e.g. cost metrics).
func momArrow(delta float64, goodIfUp bool) (arrow, class string) {
	switch {
	case delta > 0:
		if goodIfUp {
			return "↑", "kpi-good"
		}
		return "↑", "kpi-bad"
	case delta < 0:
		if goodIfUp {
			return "↓", "kpi-bad"
		}
		return "↓", "kpi-good"
	default:
		return "→", ""
	}
}

func pctChange(last, prev float64) float64 {
	if prev == 0 {
		return 0
	}
	return (last - prev) / prev * 100
}

// buildHeatmapCellView maps one RegionMonthMetrics into its HeatmapCellView
// data attributes (color is filled in separately once the grid's min/max
// are known).
func buildHeatmapCellView(m execsum.RegionMonthMetrics) HeatmapCellView {
	return HeatmapCellView{
		Month:           m.Month,
		NetRevenue:      m.NetRevenue,
		TotalVCPU:       m.VCPUHours,
		ExtVCPU:         m.ExtVCPUHours,
		IntVCPU:         m.IntVCPUHours,
		Revenue:         m.Revenue,
		MCCost:          m.MCCost,
		SCCost:          m.SCCost,
		NetworkCost:     m.NetworkCost,
		InfraCost:       m.InfraCost,
		TotalCost:       m.TotalCost,
		CostPerVCPU:     m.CostPerVCPU,
		RevenuePerVCPU:  m.RevenuePerVCPU,
		MarginPerVCPU:   m.MarginPerVCPU,
		ClusterCount:    m.ClusterCount,
		ExtClusterCount: m.ExtClusterCount,
	}
}

// buildHeatmap implements spec 8.4: rows sorted by total net revenue across
// the window descending, a "Total" column per region, default-metric
// (Net Revenue) coloring computed server-side for the initial paint, and a
// row-header sparkline of the region's last 5 months of net revenue.
func buildHeatmap(regions []execsum.RegionMonthMetrics, months []string) []HeatmapRowView {
	type cellKey struct{ region, month string }
	cellByKey := make(map[cellKey]execsum.RegionMonthMetrics, len(regions))
	regionSeen := make(map[string]bool)
	for _, m := range regions {
		cellByKey[cellKey{m.Region, m.Month}] = m
		regionSeen[m.Region] = true
	}

	type regionTotal struct {
		region     string
		netRevenue float64
	}
	totals := make(map[string]execsum.RegionMonthMetrics)
	for region := range regionSeen {
		var t execsum.RegionMonthMetrics
		t.Region = region
		for _, m := range months {
			if c, ok := cellByKey[cellKey{region, m}]; ok {
				t.NetRevenue += c.NetRevenue
				t.VCPUHours += c.VCPUHours
				t.ExtVCPUHours += c.ExtVCPUHours
				t.IntVCPUHours += c.IntVCPUHours
				t.Revenue += c.Revenue
				t.MCCost += c.MCCost
				t.SCCost += c.SCCost
				t.NetworkCost += c.NetworkCost
				t.InfraCost += c.InfraCost
				t.TotalCost += c.TotalCost
				t.ClusterCount += c.ClusterCount
				t.ExtClusterCount += c.ExtClusterCount
			}
		}
		if t.ExtVCPUHours != 0 {
			t.CostPerVCPU = t.TotalCost / t.ExtVCPUHours
			t.RevenuePerVCPU = t.Revenue / t.ExtVCPUHours
			t.MarginPerVCPU = (t.Revenue - t.TotalCost) / t.ExtVCPUHours
		}
		totals[region] = t
	}

	regionOrder := make([]regionTotal, 0, len(regionSeen))
	for region, t := range totals {
		regionOrder = append(regionOrder, regionTotal{region: region, netRevenue: t.NetRevenue})
	}
	sort.Slice(regionOrder, func(i, j int) bool {
		if regionOrder[i].netRevenue != regionOrder[j].netRevenue {
			return regionOrder[i].netRevenue > regionOrder[j].netRevenue
		}
		return regionOrder[i].region < regionOrder[j].region // deterministic tiebreak
	})

	// Default metric (Net Revenue) min/max across all populated cells AND
	// totals, scaled independently per side (spec 8.4).
	var maxPos, maxNeg float64
	consider := func(v float64) {
		if v >= 0 && v > maxPos {
			maxPos = v
		}
		if v < 0 && -v > maxNeg {
			maxNeg = -v
		}
	}
	for _, m := range regions {
		consider(m.NetRevenue)
	}
	for _, t := range totals {
		consider(t.NetRevenue)
	}

	rows := make([]HeatmapRowView, 0, len(regionOrder))
	for _, rt := range regionOrder {
		region := rt.region
		cells := make([]HeatmapCellView, len(months))
		for i, m := range months {
			c, ok := cellByKey[cellKey{region, m}]
			if !ok {
				cells[i] = HeatmapCellView{Month: m, Empty: true}
				continue
			}
			cell := buildHeatmapCellView(c)
			cell.BgColor, cell.TextColor = heatmapCellColor(c.NetRevenue, maxPos, maxNeg)
			cells[i] = cell
		}

		total := buildHeatmapCellView(totals[region])
		total.Month = "Total"
		total.BgColor, total.TextColor = heatmapCellColor(totals[region].NetRevenue, maxPos, maxNeg)

		sparkMonths := months
		if len(sparkMonths) > heatmapSparklineMonths {
			sparkMonths = sparkMonths[len(sparkMonths)-heatmapSparklineMonths:]
		}
		sparkValues := make([]float64, 0, len(sparkMonths))
		for _, m := range sparkMonths {
			if c, ok := cellByKey[cellKey{region, m}]; ok {
				sparkValues = append(sparkValues, c.NetRevenue/1000)
			} else {
				sparkValues = append(sparkValues, 0)
			}
		}

		rows = append(rows, HeatmapRowView{
			Region:       region,
			SparklineSVG: sparklineSVG(sparkValues),
			Cells:        cells,
			Total:        total,
		})
	}
	return rows
}

// buildHCPEfficiencyKPICards implements spec 7.6.2's 6 unit-economics
// cards. Tooltip content follows spec 8.5's table by exact KPI name for
// Cost/vCPU-hr (External), Margin/vCPU-hr, and External vCPU Hours; spec
// 8.5 also documents a "Revenue/vCPU-hr" tooltip that has no matching card
// among the 6 listed in 7.6.2 (a minor spec inconsistency -- 7.6.2 says
// "the first four cards" have tooltips while 8.5 names a "Revenue/vCPU-hr"
// card that doesn't exist). We attach that fourth tooltip to Net Revenue
// (the 3rd card / closest remaining revenue-related metric) so "the first
// four cards" reading of 7.6.2 still holds.
func buildHCPEfficiencyKPICards(vcpuEff []execsum.VCPUEfficiencyRecord, regions []execsum.RegionMonthMetrics, months []string) []KPICardView {
	if len(months) == 0 {
		return nil
	}
	last := months[len(months)-1]
	var prev string
	if len(months) >= 2 {
		prev = months[len(months)-2]
	}

	findClassification := func(month string, external bool) (execsum.VCPUEfficiencyRecord, bool) {
		for _, r := range vcpuEff {
			if r.Month != month {
				continue
			}
			if execsum.IsExternal(r.Classification) == external {
				return r, true
			}
		}
		return execsum.VCPUEfficiencyRecord{}, false
	}
	extLast, _ := findClassification(last, true)
	extPrev, _ := findClassification(prev, true)
	intLast, _ := findClassification(last, false)
	intPrev, _ := findClassification(prev, false)

	type monthTotals struct {
		netRevenue, totalCost, networkCost float64
		profitable, total                  int
	}
	sumForMonth := func(month string) monthTotals {
		var t monthTotals
		for _, m := range regions {
			if m.Month != month {
				continue
			}
			t.netRevenue += m.NetRevenue
			t.totalCost += m.TotalCost
			t.networkCost += m.NetworkCost
			t.total++
			if m.NetRevenue > 0 {
				t.profitable++
			}
		}
		return t
	}
	lastTotals := sumForMonth(last)
	prevTotals := sumForMonth(prev)

	costArrow, costClass := momArrow(extLast.CostPerVCPU-extPrev.CostPerVCPU, false)
	marginArrow, marginClass := momArrow(extLast.NetMarginPerVCPU-extPrev.NetMarginPerVCPU, true)
	revenueArrow, revenueClass := momArrow(lastTotals.netRevenue-prevTotals.netRevenue, true)
	totalCostArrow, totalCostClass := momArrow(lastTotals.totalCost-prevTotals.totalCost, false)
	extVCPUArrow, extVCPUClass := momArrow(extLast.VCPUHours-extPrev.VCPUHours, true)
	intVCPUArrow, _ := momArrow(intLast.VCPUHours-intPrev.VCPUHours, true) // always neutral

	valueClass := func(v float64) string {
		if v >= 0 {
			return "kpi-good"
		}
		return "kpi-bad"
	}

	networkPct := 0.0
	if lastTotals.totalCost != 0 {
		networkPct = lastTotals.networkCost / lastTotals.totalCost * 100
	}

	const costTooltip = `<strong>cost_per_vcpu = allocated_cost / external_vcpu_hours</strong><br>Where allocated_cost includes MC + SC + network infrastructure costs for regions serving customer clusters.`
	const revenueTooltip = `<strong>calculated_revenue = external_vcpu_hours × $0.04275</strong><br>Actual customer billing may differ due to discounts, credits, marketplace fees, and billing cycles. Use finance systems for real revenue data.`
	const marginTooltip = `<strong>margin_per_vcpu = (calculated_revenue - allocated_cost) / external_vcpu_hours</strong><br>A positive value indicates the service is profitable at the standard rate; negative indicates it runs at a loss per vCPU-hour based on calculated revenue.`
	const extVCPUTooltip = `<strong>external_vcpu_hours = SUM(WORKER_VCPUS_AVG × 24) WHERE classification = 'external'</strong><br>Source: OCM_MAPPING.CLASSIFICATION.`

	return []KPICardView{
		{
			Label:        "Cost/vCPU-hr (External)",
			Value:        fmt.Sprintf("$%.4f", extLast.CostPerVCPU),
			SubText:      fmt.Sprintf("%s %.1f%% vs prior month", costArrow, pctChange(extLast.CostPerVCPU, extPrev.CostPerVCPU)),
			SubTextClass: costClass,
			Tooltip:      template.HTML(costTooltip), //nolint:gosec // fixed constant, not user input
		},
		{
			Label:        "Margin/vCPU-hr",
			Value:        fmt.Sprintf("$%.4f", extLast.NetMarginPerVCPU),
			Class:        valueClass(extLast.NetMarginPerVCPU),
			SubText:      fmt.Sprintf("%s %.1f%% vs prior month", marginArrow, pctChange(extLast.NetMarginPerVCPU, extPrev.NetMarginPerVCPU)),
			SubTextClass: marginClass,
			Tooltip:      template.HTML(marginTooltip), //nolint:gosec
		},
		{
			Label:        "Net Revenue",
			Value:        format.FormatDollarCompact(lastTotals.netRevenue),
			Class:        valueClass(lastTotals.netRevenue),
			SubText:      fmt.Sprintf("%s %.1f%% vs prior month", revenueArrow, pctChange(lastTotals.netRevenue, prevTotals.netRevenue)),
			SubTextClass: revenueClass,
			Tooltip:      template.HTML(revenueTooltip), //nolint:gosec
		},
		{
			Label:         "Total Cost",
			Value:         format.FormatDollarCompact(lastTotals.totalCost),
			SubText:       fmt.Sprintf("%s %.1f%% vs prior month", totalCostArrow, pctChange(lastTotals.totalCost, prevTotals.totalCost)),
			SubTextClass:  totalCostClass,
			SubText2:      fmt.Sprintf("Network: %s (%.0f%% of total)", format.FormatDollarCompact(lastTotals.networkCost), networkPct),
			SubText2Class: "",
		},
		{
			Label:         "External vCPU Hours",
			Value:         fmt.Sprintf("%.2fM", extLast.VCPUHours/1_000_000),
			SubText:       fmt.Sprintf("%s %.1f%% vs prior month", extVCPUArrow, pctChange(extLast.VCPUHours, extPrev.VCPUHours)),
			SubTextClass:  extVCPUClass,
			SubText2:      fmt.Sprintf("Internal: %.2fM %s", intLast.VCPUHours/1_000_000, intVCPUArrow),
			SubText2Class: "",
			Tooltip:       template.HTML(extVCPUTooltip), //nolint:gosec
		},
		{
			Label:   "Profitable Regions",
			Value:   fmt.Sprintf("%d / %d", lastTotals.profitable, lastTotals.total),
			SubText: "of regions with activity",
		},
	}
}

// buildHCPEfficiencyTabView implements spec section 7.6 end to end: the 6
// KPI cards, the regional performance heatmap, and the two latest-month
// tables. No Chart.js charts are used on this tab, so the returned spec
// slice is always empty.
func buildHCPEfficiencyTabView(r execsum.HCPEfficiencyReport) (HCPEfficiencyTabView, []chartSpec) {
	mcRows := append([]execsum.MCSummaryRecord(nil), r.MCSummaryLatestMonth...)
	sort.Slice(mcRows, func(i, j int) bool { return mcRows[i].MCCost > mcRows[j].MCCost })
	if len(mcRows) > mcSummaryTableLimit {
		mcRows = mcRows[:mcSummaryTableLimit]
	}
	mcViews := make([]MCSummaryRowView, 0, len(mcRows))
	for _, m := range mcRows {
		mcViews = append(mcViews, MCSummaryRowView{
			MCName:             m.MCName,
			SCName:             m.SCName,
			Region:             m.MCRegion,
			MCCostFormatted:    format.FormatDollarCompact(m.MCCost),
			CustomerCount:      m.CustomerCount,
			InternalCount:      m.InternalCount,
			VCPUHoursFormatted: fmt.Sprintf("%.0f", m.VCPUHours),
		})
	}

	clusterRows := append([]execsum.ClusterDetailRecord(nil), r.ClusterDetailLatestMonth...)
	sort.Slice(clusterRows, func(i, j int) bool { return clusterRows[i].VCPUHours > clusterRows[j].VCPUHours })
	if len(clusterRows) > clusterDetailTableLimit {
		clusterRows = clusterRows[:clusterDetailTableLimit]
	}
	clusterViews := make([]ClusterDetailRowView, 0, len(clusterRows))
	for _, c := range clusterRows {
		badge := BadgeView{Label: c.Classification, Class: "badge-medium"}
		if execsum.IsExternal(c.Classification) {
			badge.Class = "badge-high"
		}
		clusterViews = append(clusterViews, ClusterDetailRowView{
			ClusterID:          c.ClusterID,
			MCName:             c.MCName,
			Region:             c.Region,
			Classification:     badge,
			Organization:       c.Organization,
			BillingModel:       c.BillingModel,
			VCPUHoursFormatted: fmt.Sprintf("%.0f", c.VCPUHours),
			RevenueFormatted:   format.FormatDollarCompact(c.Revenue),
		})
	}

	tab := HCPEfficiencyTabView{
		KPICards:          buildHCPEfficiencyKPICards(r.VCPUEfficiency, r.Regions, r.Months),
		Months:            r.Months,
		HeatmapRows:       buildHeatmap(r.Regions, r.Months),
		MCSummaryRows:     mcViews,
		ClusterDetailRows: clusterViews,
	}
	return tab, nil
}
