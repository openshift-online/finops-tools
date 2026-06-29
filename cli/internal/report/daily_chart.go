package report

import (
	"fmt"
	"math"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	"github.com/openshift-online/finops-tools/core/cost"
)

const (
	chartWidth   = 900
	chartHeight  = 300
	chartPadR    = 24
	chartPadT    = 20
	chartPadB    = 52
	minChartPadL = 64
	maxChartPadL = 140
	// Extra viewBox margin so edge labels are not clipped when the SVG scales.
	viewMarginLeft   = 4
	viewMarginRight  = 4
	viewMarginBottom = 4
)

// dailyChartSVG renders a self-contained line chart (no JavaScript) for daily costs.
func dailyChartSVG(daily []cost.DailyCostItem, currency string) string {
	if len(daily) == 0 {
		return `<p class="meta">No daily cost data for this period.</p>`
	}

	cur := strings.TrimSpace(currency)
	if cur == "" {
		cur = "USD"
	}

	maxAmt := 0.0
	for _, d := range daily {
		if d.Amount > maxAmt {
			maxAmt = d.Amount
		}
	}
	if maxAmt <= 0 {
		maxAmt = 1
	}

	padL := yAxisPadLeft(maxAmt, cur)
	plotW := float64(chartWidth - padL - chartPadR)
	plotH := float64(chartHeight - chartPadT - chartPadB)

	points := make([]string, len(daily))
	baseY := chartPadT + int(plotH)
	area := make([]string, 0, len(daily)+2)

	for i, d := range daily {
		x := padL + int(plotW*float64(i)/math.Max(float64(len(daily)-1), 1))
		y := chartPadT + int(plotH*(1-d.Amount/maxAmt))
		points[i] = fmt.Sprintf("%d,%d", x, y)
		if i == 0 {
			area = append(area, fmt.Sprintf("%d,%d", x, baseY))
		}
		area = append(area, fmt.Sprintf("%d,%d", x, y))
	}
	lastX := padL + int(plotW*float64(len(daily)-1)/math.Max(float64(len(daily)-1), 1))
	area = append(area, fmt.Sprintf("%d,%d", lastX, baseY))

	viewW := chartWidth + viewMarginLeft + viewMarginRight
	viewH := chartHeight + viewMarginBottom

	var b strings.Builder
	b.WriteString(`<svg class="daily-chart" overflow="visible" viewBox="`)
	b.WriteString(fmt.Sprintf("%d 0 %d %d", -viewMarginLeft, viewW, viewH))
	b.WriteString(`" role="img" aria-label="Daily net amortized costs">`)

	yLabelX := padL - 10
	for _, frac := range []float64{0, 0.5, 1} {
		y := chartPadT + int(plotH*(1-frac))
		val := maxAmt * frac
		b.WriteString(fmt.Sprintf(`<line class="chart-grid" x1="%d" y1="%d" x2="%d" y2="%d"/>`,
			padL, y, chartWidth-chartPadR, y))
		b.WriteString(fmt.Sprintf(
			`<text class="chart-axis" x="%d" y="%d" text-anchor="end" dominant-baseline="middle">%s</text>`,
			yLabelX, y, htmlEscape(formatChartAmount(val, cur))))
	}

	b.WriteString(fmt.Sprintf(`<polygon class="chart-area" points="%s"/>`, strings.Join(area, " ")))
	b.WriteString(fmt.Sprintf(`<polyline class="chart-line" points="%s"/>`, strings.Join(points, " ")))

	for i, d := range daily {
		x := padL + int(plotW*float64(i)/math.Max(float64(len(daily)-1), 1))
		y := chartPadT + int(plotH*(1-d.Amount/maxAmt))
		b.WriteString(fmt.Sprintf(`<circle class="chart-point" cx="%d" cy="%d" r="3"><title>%s: %s</title></circle>`,
			x, y, htmlEscape(d.Date), htmlEscape(formatChartAmount(d.Amount, cur))))
	}

	labelIdx := xLabelIndices(len(daily))
	xLabelY := chartHeight - chartPadB + 28
	for _, i := range labelIdx {
		x := padL + int(plotW*float64(i)/math.Max(float64(len(daily)-1), 1))
		anchor := xLabelAnchor(i, len(daily))
		b.WriteString(fmt.Sprintf(
			`<text class="chart-axis" x="%d" y="%d" text-anchor="%s">%s</text>`,
			x, xLabelY, anchor, htmlEscape(daily[i].Date)))
	}

	b.WriteString(`</svg>`)
	return b.String()
}

func yAxisPadLeft(maxAmt float64, currency string) int {
	maxLen := 0
	for _, frac := range []float64{0, 0.5, 1} {
		n := len(formatChartAmount(maxAmt*frac, currency))
		if n > maxLen {
			maxLen = n
		}
	}
	// Approximate width at 11px sans-serif (~6px per char) plus gap before plot.
	w := maxLen*6 + 24
	if w < minChartPadL {
		return minChartPadL
	}
	if w > maxChartPadL {
		return maxChartPadL
	}
	return w
}

func xLabelAnchor(index, total int) string {
	switch {
	case index == 0:
		return "start"
	case index == total-1:
		return "end"
	default:
		return "middle"
	}
}

func xLabelIndices(n int) []int {
	switch {
	case n <= 1:
		return []int{0}
	case n <= 4:
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	default:
		mid := n / 2
		return []int{0, mid, n - 1}
	}
}

func formatChartAmount(amount float64, currency string) string {
	return format.FormatChartMoney(amount, currency)
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
