package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	corereport "github.com/openshift-online/finops-tools/core/report"
)

func TestRenderCostsHTMLDailyChart(t *testing.T) {
	var buf bytes.Buffer
	err := RenderCostsHTML(&buf, corereport.CostsReport{
		GeneratedAt: time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC),
		StartDate:   "2026-04-25",
		EndDate:     "2026-05-24",
		Currency:    "USD",
		Metric:      "NetAmortizedCost",
		Total:       100,
		Daily: []cost.DailyCostItem{
			{Date: "2026-05-23", Amount: 30},
			{Date: "2026-05-24", Amount: 40},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`<svg class="daily-chart"`,
		`<polyline class="chart-line"`,
		"2026-05-23",
		"2026-05-24",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Contains(out, "chart.js") || strings.Contains(out, "dailyChart") {
		t.Error("expected embedded SVG chart, not Chart.js canvas")
	}
}
