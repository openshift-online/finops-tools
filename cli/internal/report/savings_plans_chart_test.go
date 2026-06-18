package report

import (
	"strings"
	"testing"
)

func TestFormatCompactUSD(t *testing.T) {
	tests := []struct {
		amount float64
		want   string
	}{
		{7_912_617, "$7.91M"},
		{54_919.59, "$54.9K"},
		{1_500, "$1.50K"},
		{500, "$500"},
		{-1_000_000, "-$1.00M"},
	}
	for _, tt := range tests {
		got := formatCompactUSD(tt.amount)
		if got != tt.want {
			t.Errorf("formatCompactUSD(%v) = %q, want %q", tt.amount, got, tt.want)
		}
	}
}

func TestDashboardCoverageStatus(t *testing.T) {
	label, class := dashboardCoverageStatus(95.8)
	if label != "Good" || class != "status-good" {
		t.Errorf("got %q %q", label, class)
	}
	label, class = dashboardCoverageStatus(94.4)
	if label != "Watch" || class != "status-watch" {
		t.Errorf("got %q %q", label, class)
	}
	label, class = dashboardCoverageStatus(75.9)
	if label != "Poor" || class != "status-poor" {
		t.Errorf("got %q %q", label, class)
	}
}

func TestDashboardUtilizationStatus(t *testing.T) {
	label, class := dashboardUtilizationStatus(99.9)
	if label != "Good" || class != "status-good" {
		t.Errorf("got %q %q", label, class)
	}
	label, class = dashboardUtilizationStatus(92.9)
	if label != "Watch" || class != "status-watch" {
		t.Errorf("got %q %q", label, class)
	}
	label, class = dashboardUtilizationStatus(69.9)
	if label != "Poor" || class != "status-poor" {
		t.Errorf("got %q %q", label, class)
	}
}

func TestSpBubbleChartSVG(t *testing.T) {
	svg := spBubbleChartSVG([]spBubblePoint{{
		Label:          "Production",
		Coverage:       95.8,
		Utilization:    99.9,
		Savings:        6_742_319,
		SavingsCompact: "$6.74M",
		Color:          "#1a73e8",
	}})
	for _, want := range []string{
		`<svg class="sp-bubble-chart"`,
		"Production",
		"$6.74M",
		`<circle`,
		`sp-bubble-callout-bg`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("bubble chart missing %q", want)
		}
	}
}

func TestSpBubbleChartSVG_threeAccounts(t *testing.T) {
	svg := spBubbleChartSVG([]spBubblePoint{
		{Label: "Service Delivery Production Org", Coverage: 95.8, Utilization: 99.9, Savings: 6_742_319, SavingsCompact: "$6.74M", Color: "#1a73e8"},
		{Label: "osd staging 1", Coverage: 94.4, Utilization: 92.9, Savings: 1_115_378, SavingsCompact: "$1.12M", Color: "#ed6c02"},
		{Label: "osd staging 2", Coverage: 75.9, Utilization: 98.0, Savings: 54_920, SavingsCompact: "$54.9K", Color: "#2e7d32"},
	})
	for _, want := range []string{
		"Production",
		"Staging 1",
		"Staging 2",
		"$6.74M",
		"$1.12M",
		"$54.9K",
		`sp-bubble-callout-bg`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("three-account chart missing %q", want)
		}
	}
	if strings.Contains(svg, "High cov") || strings.Contains(svg, "Low cov") {
		t.Error("quadrant hint labels should be removed")
	}
}

func TestSpBubbleChartSVG_empty(t *testing.T) {
	svg := spBubbleChartSVG(nil)
	if !strings.Contains(svg, "No account data") {
		t.Errorf("got %q", svg)
	}
}

func TestShortBubbleLabel(t *testing.T) {
	if got := shortBubbleLabel("Service Delivery Production Org"); got != "Production" {
		t.Errorf("got %q", got)
	}
	if got := shortBubbleLabel("osd staging 1"); got != "Staging 1" {
		t.Errorf("got %q", got)
	}
	longUTF8 := "あいうえおかきくけこさしすせそ"
	if got := shortBubbleLabel(longUTF8); got != "あいうえおかきくけこさし…" {
		t.Errorf("UTF-8 truncation: got %q", got)
	}
}

func TestSpProgressRingSVG(t *testing.T) {
	svg := spProgressRingSVG(95.8, "status-good", 72)
	if !strings.Contains(svg, "95.8%") || !strings.Contains(svg, `#2e7d32`) {
		t.Errorf("ring svg unexpected: %s", svg)
	}
}
