package report

import (
	"fmt"
	"math"
	"strings"
)

const (
	spBubbleChartW     = 520
	spBubbleChartH     = 380
	spBubblePadL       = 56
	spBubblePadR       = 24
	spBubblePadT       = 28
	spBubblePadB       = 52
	spBubbleViewMargin = 16 // extra viewBox room for outside labels
)

var spAccountColors = []string{
	"#1a73e8", // blue
	"#ed6c02", // orange
	"#2e7d32", // green
	"#7b1fa2", // purple
	"#c62828", // red
	"#00838f", // teal
}

type spBubblePoint struct {
	Label          string
	Coverage       float64
	Utilization    float64
	Savings        float64
	SavingsCompact string
	Color          string
}

type spBubbleLabelLayout struct {
	x, nameY, savingsY float64
	anchor             string
	show               bool
	leaderX1, leaderY1 float64
	leaderX2, leaderY2 float64
	showLeader         bool
}

type spCalloutRect struct {
	x, y, w, h float64
}

type spBubbleLabelDir struct {
	ux, uy float64
	anchor string
}

type spBubbleDraw struct {
	p      spBubblePoint
	x, y   int
	r      float64
	layout spBubbleLabelLayout
}

// spBubbleChartSVG renders a coverage vs utilization bubble chart (no JavaScript).
func spBubbleChartSVG(points []spBubblePoint) string {
	if len(points) == 0 {
		return `<p class="meta">No account data for chart.</p>`
	}

	const (
		covMin  = 60.0
		covMax  = 100.0
		utilMin = 80.0
		utilMax = 100.0
		refCov  = 95.0
		refUtil = 95.0
	)

	plotW := float64(spBubbleChartW - spBubblePadL - spBubblePadR)
	plotH := float64(spBubbleChartH - spBubblePadT - spBubblePadB)

	maxSavings := 0.0
	for _, p := range points {
		if p.Savings > maxSavings {
			maxSavings = p.Savings
		}
	}
	if maxSavings <= 0 {
		maxSavings = 1
	}

	toX := func(cov float64) int {
		cov = math.Max(covMin, math.Min(covMax, cov))
		return spBubblePadL + int(plotW*(cov-covMin)/(covMax-covMin))
	}
	toY := func(util float64) int {
		util = math.Max(utilMin, math.Min(utilMax, util))
		return spBubblePadT + int(plotH*(1-(util-utilMin)/(utilMax-utilMin)))
	}
	bubbleR := func(savings float64) float64 {
		minR, maxR := 18.0, 52.0
		if maxSavings <= 0 || savings <= 0 {
			return minR
		}
		frac := math.Sqrt(savings / maxSavings)
		return minR + (maxR-minR)*frac
	}

	var b strings.Builder
	viewW := spBubbleChartW + 2*spBubbleViewMargin
	viewH := spBubbleChartH + 2*spBubbleViewMargin
	b.WriteString(`<svg class="sp-bubble-chart" overflow="visible" viewBox="`)
	fmt.Fprintf(&b, "%d %d %d %d", -spBubbleViewMargin, -spBubbleViewMargin, viewW, viewH)
	b.WriteString(`" role="img" aria-label="Coverage vs utilization by account">`)
	fmt.Fprintf(&b, `<g transform="translate(0,0)">`)

	plotLeft := float64(spBubblePadL)
	plotTop := float64(spBubblePadT)
	plotRight := float64(spBubbleChartW - spBubblePadR)
	plotBottom := float64(spBubbleChartH - spBubblePadB)
	plotCenterX := plotLeft + plotW/2
	plotCenterY := plotTop + plotH/2

	// Quadrant shading (under-covered, high utilization)
	refX := toX(refCov)
	refY := toY(refUtil)
	fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="rgba(237,108,2,0.08)" rx="2"/>`,
		refX, spBubblePadT, spBubbleChartW-spBubblePadR-refX, refY-spBubblePadT)

	// Grid lines
	for _, pct := range []float64{60, 70, 80, 90, 100} {
		x := toX(pct)
		fmt.Fprintf(&b, `<line class="sp-chart-grid" x1="%d" y1="%d" x2="%d" y2="%d"/>`, x, spBubblePadT, x, spBubblePadT+int(plotH))
		fmt.Fprintf(&b, `<text class="sp-chart-axis" x="%d" y="%d" text-anchor="middle">%.0f%%</text>`, x, spBubbleChartH-12, pct)
	}
	for _, pct := range []float64{80, 85, 90, 95, 100} {
		y := toY(pct)
		fmt.Fprintf(&b, `<line class="sp-chart-grid" x1="%d" y1="%d" x2="%d" y2="%d"/>`, spBubblePadL, y, spBubblePadL+int(plotW), y)
		fmt.Fprintf(&b, `<text class="sp-chart-axis" x="%d" y="%d" text-anchor="end" dominant-baseline="middle">%.0f%%</text>`, spBubblePadL-8, y, pct)
	}

	// Reference lines
	fmt.Fprintf(&b, `<line class="sp-chart-ref" x1="%d" y1="%d" x2="%d" y2="%d"/>`, refX, spBubblePadT, refX, spBubblePadT+int(plotH))
	fmt.Fprintf(&b, `<line class="sp-chart-ref" x1="%d" y1="%d" x2="%d" y2="%d"/>`, spBubblePadL, refY, spBubblePadL+int(plotW), refY)

	// Axis titles
	fmt.Fprintf(&b, `<text class="sp-chart-axis-title" x="%d" y="%d" text-anchor="middle">COVERAGE</text>`, spBubblePadL+int(plotW/2), spBubbleChartH-2)
	fmt.Fprintf(&b, `<text class="sp-chart-axis-title" x="14" y="%d" text-anchor="middle" transform="rotate(-90 14 %d)">UTILIZATION</text>`, spBubblePadT+int(plotH/2), spBubblePadT+int(plotH/2))

	// Bubbles (largest first so smaller ones render on top)
	sorted := append([]spBubblePoint(nil), points...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Savings > sorted[i].Savings {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	type bubbleDraw = spBubbleDraw
	drawn := make([]bubbleDraw, 0, len(points))
	for _, p := range points {
		x := toX(p.Coverage)
		y := toY(p.Utilization)
		r := bubbleR(p.Savings)
		drawn = append(drawn, bubbleDraw{
			p: p,
			x: x,
			y: y,
			r: r,
		})
	}
	spBubbleLayoutCallouts(drawn, plotLeft, plotTop, plotRight, plotBottom, plotCenterX, plotCenterY)
	for _, d := range sorted {
		x := toX(d.Coverage)
		y := toY(d.Utilization)
		r := bubbleR(d.Savings)
		b.WriteString(fmt.Sprintf(
			`<circle cx="%d" cy="%d" r="%.1f" fill="%s" fill-opacity="0.82" stroke="#fff" stroke-width="2"><title>%s: %.1f%% coverage, %.1f%% utilization, %s saved</title></circle>`,
			x, y, r, htmlEscape(d.Color), htmlEscape(d.Label), d.Coverage, d.Utilization, htmlEscape(d.SavingsCompact)))
	}

	// Callout labels on top (smallest bubbles last so their labels stay visible)
	for i := 0; i < len(drawn); i++ {
		for j := i + 1; j < len(drawn); j++ {
			if drawn[j].r < drawn[i].r {
				drawn[i], drawn[j] = drawn[j], drawn[i]
			}
		}
	}
	for _, d := range drawn {
		if !d.layout.show {
			continue
		}
		label := shortBubbleLabel(d.p.Label)
		spBubbleWriteCallout(&b, d.layout, label, d.p.SavingsCompact, d.p.Color)
	}

	b.WriteString(`</g></svg>`)
	return b.String()
}

var spBubbleLabelDirs = []spBubbleLabelDir{
	{0, -1, "middle"},
	{-1, 0, "end"},
	{1, 0, "start"},
	{0, 1, "middle"},
	{-0.75, -0.65, "end"},
	{0.75, -0.65, "start"},
	{-0.75, 0.55, "end"},
	{0.75, 0.55, "start"},
	{-0.45, -0.9, "middle"},
	{0.45, -0.9, "middle"},
}

func spBubbleLayoutCallouts(
	drawn []spBubbleDraw,
	plotLeft, plotTop, plotRight, plotBottom, plotCenterX, plotCenterY float64,
) {
	order := make([]int, len(drawn))
	for i := range order {
		order[i] = i
	}
	// Smallest bubbles first — they get first pick of non-overlapping label slots.
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if drawn[order[j]].r < drawn[order[i]].r {
				order[i], order[j] = order[j], order[i]
			}
		}
	}

	placed := make([]spCalloutRect, 0, len(drawn))
	for _, idx := range order {
		d := &drawn[idx]
		name := shortBubbleLabel(d.p.Label)
		savings := d.p.SavingsCompact
		bestScore := -1e9
		var best spBubbleLabelLayout

		for _, dir := range spBubbleLabelDirs {
			layout := spBubbleLabelAtDirection(
				d.x, d.y, d.r, name, savings, dir,
				plotLeft, plotTop, plotRight, plotBottom,
			)
			rect := spBubbleCalloutRect(layout, name, savings)
			score := spBubbleLabelScore(
				d.x, d.y, d.r, rect, placed, drawn, idx,
				plotLeft, plotTop, plotRight, plotBottom,
				plotCenterX, plotCenterY, dir,
			)
			if score > bestScore {
				bestScore = score
				best = layout
			}
		}

		if bestScore > -1e8 {
			best.show = true
			rect := spBubbleCalloutRect(best, name, savings)
			edgeX, edgeY := spBubbleRimPoint(float64(d.x), float64(d.y), d.r, best.x, best.nameY+6)
			best.leaderX1, best.leaderY1 = edgeX, edgeY
			best.leaderX2, best.leaderY2 = best.x, best.nameY+6
			best.showLeader = math.Hypot(best.x-float64(d.x), best.nameY+6-float64(d.y)) > d.r+8
			d.layout = best
			placed = append(placed, rect)
		}
	}
}

func spBubbleLabelAtDirection(
	cx, cy int, r float64,
	name, savings string,
	dir spBubbleLabelDir,
	plotLeft, plotTop, plotRight, plotBottom float64,
) spBubbleLabelLayout {
	boxW := math.Max(approxTextWidth(name, 11), approxTextWidth(savings, 9)) + 10
	if boxW < 48 {
		boxW = 48
	}
	boxH := 26.0

	mag := math.Hypot(dir.ux, dir.uy)
	ux, uy := dir.ux/mag, dir.uy/mag
	lead := r + 16 + boxH/2
	x := float64(cx) + ux*lead
	nameY := float64(cy) + uy*lead - 6
	savingsY := nameY + 12
	anchor := dir.anchor

	rect := spCalloutRect{
		w: boxW,
		h: boxH,
	}
	switch anchor {
	case "start":
		rect.x = x - 4
	case "end":
		rect.x = x - boxW + 4
	default:
		rect.x = x - boxW/2
	}
	rect.y = nameY - 10

	// Nudge fully inside plot.
	const pad = 4.0
	if rect.x < plotLeft+pad {
		shift := plotLeft + pad - rect.x
		rect.x += shift
		x += shift
	}
	if rect.x+rect.w > plotRight-pad {
		shift := rect.x + rect.w - (plotRight - pad)
		rect.x -= shift
		x -= shift
	}
	if rect.y < plotTop+pad {
		shift := plotTop + pad - rect.y
		rect.y += shift
		nameY += shift
		savingsY += shift
	}
	if rect.y+rect.h > plotBottom-pad {
		shift := rect.y + rect.h - (plotBottom - pad)
		rect.y -= shift
		nameY -= shift
		savingsY -= shift
	}

	return spBubbleLabelLayout{
		x:        x,
		nameY:    nameY,
		savingsY: savingsY,
		anchor:   anchor,
	}
}

func spBubbleCalloutRect(layout spBubbleLabelLayout, name, savings string) spCalloutRect {
	boxW := math.Max(approxTextWidth(name, 11), approxTextWidth(savings, 9)) + 10
	if boxW < 48 {
		boxW = 48
	}
	boxH := 26.0
	return spCalloutRect{
		x: spBubbleCalloutBoxX(layout.x, boxW, layout.anchor),
		y: layout.nameY - 10,
		w: boxW,
		h: boxH,
	}
}

func spBubbleLabelScore(
	cx, cy int, r float64,
	rect spCalloutRect,
	placed []spCalloutRect,
	drawn []spBubbleDraw,
	selfIdx int,
	plotLeft, plotTop, plotRight, plotBottom, plotCenterX, plotCenterY float64,
	dir spBubbleLabelDir,
) float64 {
	score := 100.0

	if rect.x < plotLeft || rect.y < plotTop || rect.x+rect.w > plotRight || rect.y+rect.h > plotBottom {
		score -= 500
	}

	for i, other := range drawn {
		if i == selfIdx {
			continue
		}
		if spCircleRectOverlap(float64(other.x), float64(other.y), other.r+3, rect) {
			score -= 800
		}
	}

	for _, p := range placed {
		if spRectOverlap(rect, p, 2) {
			score -= 600
		}
	}

	// Prefer placing labels away from the plot center and toward chart edges.
	distFromCenter := math.Hypot(rect.x+rect.w/2-plotCenterX, rect.y+rect.h/2-plotCenterY)
	score += distFromCenter * 0.05

	// Side-aware preference: left-side bubbles → left labels, etc.
	if float64(cx) < plotCenterX && dir.ux < 0 {
		score += 30
	}
	if float64(cx) > plotCenterX && dir.ux > 0 {
		score += 30
	}
	if float64(cy) < plotCenterY && dir.uy < 0 {
		score += 20
	}
	if float64(cy) > plotCenterY && dir.uy > 0 {
		score += 20
	}

	// Slight preference for above/below over diagonal when bubbles cluster top-right.
	if dir.ux == 0 || dir.uy == 0 {
		score += 10
	}

	return score
}

func spCircleRectOverlap(cx, cy, r float64, rect spCalloutRect) bool {
	nx := math.Max(rect.x, math.Min(cx, rect.x+rect.w))
	ny := math.Max(rect.y, math.Min(cy, rect.y+rect.h))
	return math.Hypot(cx-nx, cy-ny) < r
}

func spRectOverlap(a, b spCalloutRect, pad float64) bool {
	return a.x < b.x+b.w+pad &&
		a.x+a.w+pad > b.x &&
		a.y < b.y+b.h+pad &&
		a.y+a.h+pad > b.y
}

func spBubbleRimPoint(cx, cy, r, tx, ty float64) (float64, float64) {
	dx := tx - cx
	dy := ty - cy
	mag := math.Hypot(dx, dy)
	if mag < 1 {
		return cx, cy - r
	}
	return cx + dx/mag*r, cy + dy/mag*r
}

func spBubbleWriteCallout(b *strings.Builder, pos spBubbleLabelLayout, name, savings, color string) {
	nameW := approxTextWidth(name, 11)
	savingsW := approxTextWidth(savings, 9)
	boxW := math.Max(nameW, savingsW) + 10
	boxH := 26.0
	boxX := spBubbleCalloutBoxX(pos.x, boxW, pos.anchor)
	boxY := pos.nameY - 10

	if pos.showLeader {
		b.WriteString(fmt.Sprintf(
			`<line class="sp-bubble-leader" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`,
			pos.leaderX1, pos.leaderY1, pos.leaderX2, pos.leaderY2))
	}
	b.WriteString(fmt.Sprintf(
		`<rect class="sp-bubble-callout-bg" x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="4"/>`,
		boxX, boxY, boxW, boxH))
	b.WriteString(fmt.Sprintf(
		`<text class="sp-bubble-label" x="%.1f" y="%.1f" text-anchor="%s" fill="%s">%s</text>`,
		pos.x, pos.nameY, pos.anchor, htmlEscape(color), htmlEscape(name)))
	b.WriteString(fmt.Sprintf(
		`<text class="sp-bubble-sublabel" x="%.1f" y="%.1f" text-anchor="%s">%s</text>`,
		pos.x, pos.savingsY, pos.anchor, htmlEscape(savings)))
}

func spBubbleCalloutBoxX(textX, boxW float64, anchor string) float64 {
	switch anchor {
	case "start":
		return textX - 4
	case "end":
		return textX - boxW + 4
	default:
		return textX - boxW/2
	}
}

func approxTextWidth(text string, fontSize float64) float64 {
	if text == "" {
		return 0
	}
	return float64(len(text)) * fontSize * 0.58
}

func shortBubbleLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Account"
	}
	lower := strings.ToLower(name)
	if strings.Contains(lower, "production") {
		return "Production"
	}
	for _, prefix := range []string{"osd staging ", "staging "} {
		if idx := strings.Index(lower, prefix); idx >= 0 {
			rest := strings.TrimSpace(name[idx+len(prefix):])
			if rest != "" {
				return "Staging " + rest
			}
		}
	}
	runes := []rune(name)
	if len(runes) <= 14 {
		return name
	}
	return string(runes[:12]) + "…"
}

// spProgressRingSVG renders a circular progress indicator for coverage or utilization.
func spProgressRingSVG(pct float64, statusClass string, size int) string {
	stroke := spStatusColor(statusClass)
	r := float64(size)/2 - 7
	cx := float64(size) / 2
	cy := float64(size) / 2
	circ := 2 * math.Pi * r
	clamped := math.Max(0, math.Min(100, pct))
	offset := circ * (1 - clamped/100)

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg class="sp-ring" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-hidden="true">`, size, size, size, size))
	b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="none" stroke="#e8eaed" stroke-width="6"/>`, cx, cy, r))
	b.WriteString(fmt.Sprintf(
		`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="none" stroke="%s" stroke-width="6" stroke-linecap="round" transform="rotate(-90 %.1f %.1f)" stroke-dasharray="%.2f" stroke-dashoffset="%.2f"/>`,
		cx, cy, r, stroke, cx, cy, circ, offset))
	b.WriteString(fmt.Sprintf(`<text class="sp-ring-value" x="%.1f" y="%.1f" text-anchor="middle" dominant-baseline="middle">%.1f%%</text>`, cx, cy, pct))
	b.WriteString(`</svg>`)
	return b.String()
}

// spSavingsDonutSVG renders a small donut chart for savings vs on-demand percentage.
func spSavingsDonutSVG(pct float64, color string, size int) string {
	if color == "" {
		color = "#1a73e8"
	}
	r := float64(size)/2 - 4
	cx := float64(size) / 2
	cy := float64(size) / 2
	circ := 2 * math.Pi * r
	clamped := math.Max(0, math.Min(100, pct))
	offset := circ * (1 - clamped/100)

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg class="sp-donut" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-hidden="true">`, size, size, size, size))
	b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="none" stroke="#e8eaed" stroke-width="5"/>`, cx, cy, r))
	b.WriteString(fmt.Sprintf(
		`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="none" stroke="%s" stroke-width="5" stroke-linecap="round" transform="rotate(-90 %.1f %.1f)" stroke-dasharray="%.2f" stroke-dashoffset="%.2f"/>`,
		cx, cy, r, color, cx, cy, circ, offset))
	b.WriteString(`</svg>`)
	return b.String()
}

func spStatusColor(class string) string {
	switch class {
	case "status-good":
		return "#2e7d32"
	case "status-watch":
		return "#ed6c02"
	case "status-poor":
		return "#d32f2f"
	default:
		return "#656d76"
	}
}

func formatCompactUSD(amount float64) string {
	neg := amount < 0
	if neg {
		amount = -amount
	}
	prefix := "$"
	if neg {
		prefix = "-$"
	}
	switch {
	case amount >= 1_000_000_000:
		return fmt.Sprintf("%s%.2fB", prefix, amount/1_000_000_000)
	case amount >= 1_000_000:
		return fmt.Sprintf("%s%.2fM", prefix, amount/1_000_000)
	case amount >= 10_000:
		return fmt.Sprintf("%s%.1fK", prefix, amount/1_000)
	case amount >= 1_000:
		return fmt.Sprintf("%s%.2fK", prefix, amount/1_000)
	default:
		return fmt.Sprintf("%s%.0f", prefix, amount)
	}
}
