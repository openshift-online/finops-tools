package report

import (
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/cost"
)

func TestDailyChartSVG(t *testing.T) {
	svg := dailyChartSVG([]cost.DailyCostItem{
		{Date: "2026-05-22", Amount: 10},
		{Date: "2026-05-23", Amount: 30},
		{Date: "2026-05-24", Amount: 20},
	}, "USD")
	for _, want := range []string{
		`<svg class="daily-chart"`,
		`<polyline class="chart-line"`,
		`<polygon class="chart-area"`,
		"2026-05-22",
		"2026-05-24",
		"USD",
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("SVG missing %q:\n%s", want, svg)
		}
	}
}

func TestDailyChartSVGLargeYLabels(t *testing.T) {
	svg := dailyChartSVG([]cost.DailyCostItem{{Date: "2026-05-24", Amount: 125000}}, "USD")
	if !strings.Contains(svg, `overflow="visible"`) {
		t.Error("expected overflow visible on svg")
	}
	if !strings.Contains(svg, `text-anchor="start"`) || !strings.Contains(svg, `text-anchor="end"`) {
		t.Error("expected edge-aligned x labels")
	}
	if !strings.Contains(svg, "125,000") {
		t.Errorf("expected thousands separator in y-axis label:\n%s", svg)
	}
}

func TestXLabelAnchor(t *testing.T) {
	if xLabelAnchor(0, 30) != "start" || xLabelAnchor(29, 30) != "end" || xLabelAnchor(15, 30) != "middle" {
		t.Fatal("unexpected anchors")
	}
}

func TestDailyChartSVGEmpty(t *testing.T) {
	svg := dailyChartSVG(nil, "USD")
	if !strings.Contains(svg, "No daily cost data") {
		t.Fatalf("got %q", svg)
	}
}

func TestHtmlEscape(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"O'Brien", "O&#39;Brien"},
		{`a&b<c>d"e'f`, "a&amp;b&lt;c&gt;d&quot;e&#39;f"},
	}
	for _, tt := range tests {
		if got := htmlEscape(tt.in); got != tt.want {
			t.Errorf("htmlEscape(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
