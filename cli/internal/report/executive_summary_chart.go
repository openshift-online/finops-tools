package report

import (
	"bytes"
	"encoding/json"
	"html/template"
)

// execSummaryPalette is the Red Hat 10-color palette used to cycle through
// chart segments and category/product colors (spec 7.2).
var execSummaryPalette = []string{
	"#ee0000", "#37a3a3", "#f5921b", "#63993d", "#0066cc",
	"#5e40be", "#a60000", "#63bdbd", "#f8ae54", "#87bb62",
}

// paletteColor returns the palette color for the given rank, cycling
// through execSummaryPalette.
func paletteColor(rank int) string {
	if rank < 0 {
		rank = 0
	}
	return execSummaryPalette[rank%len(execSummaryPalette)]
}

// clusterGrowthProductColors is the spec 7.7 fixed product color mapping;
// any product not listed here falls back to the palette.
var clusterGrowthProductColors = map[string]string{
	"ROSA HCP":     "#ee0000",
	"ROSA Classic": "#f5921b",
	"OSD":          "#37a3a3",
	"OSD Trial":    "#f8ae54",
}

// productColor returns a product type's fixed color (spec 7.7), or a
// palette color keyed by rank if the product isn't in the fixed mapping.
func productColor(product string, fallbackRank int) string {
	if c, ok := clusterGrowthProductColors[product]; ok {
		return c
	}
	return paletteColor(fallbackRank)
}

// chartDataset is one Chart.js dataset, declaratively described for the
// template's generic renderCharts() bootstrap (spec 8.3).
type chartDataset struct {
	Label string     `json:"label"`
	Data  []*float64 `json:"data"` // nil entries render as gaps (spanGaps)
	Color string     `json:"color"`
}

// chartSpec declaratively describes one Chart.js chart for the template's
// generic renderCharts() bootstrap (spec 8.3): every chart on the page is
// rendered by the same small piece of vendored-Chart.js-driving JS, fed by
// one of these specs per <canvas>.
type chartSpec struct {
	CanvasID    string         `json:"canvasId"`
	Type        string         `json:"type"` // "bar" | "line"
	Stacked     bool           `json:"stacked,omitempty"`
	Labels      []string       `json:"labels"`
	Datasets    []chartDataset `json:"datasets"`
	YMin        *float64       `json:"yMin,omitempty"`
	YMax        *float64       `json:"yMax,omitempty"`
	YTickFormat string         `json:"yTickFormat,omitempty"` // "dollarK" | "percent" | "signed"
	SpanGaps    bool           `json:"spanGaps,omitempty"`
	PointRadius *int           `json:"pointRadius,omitempty"`
	Toggleable  bool           `json:"toggleable,omitempty"`
}

func floatPtr(v float64) *float64 { return &v }

func intPtr(v int) *int { return &v }

// chartSpecsJS marshals specs to a JSON array embedded as a trusted JS
// literal (spec's vendored-Chart.js approach -- no per-report remote
// fetch). "</script" is escaped defensively even though none of this
// report's data is expected to contain it.
func chartSpecsJS(specs []chartSpec) (template.JS, error) {
	b, err := json.Marshal(specs)
	if err != nil {
		return "", err
	}
	b = bytes.ReplaceAll(b, []byte("</script"), []byte("<\\/script"))
	return template.JS(b), nil
}
