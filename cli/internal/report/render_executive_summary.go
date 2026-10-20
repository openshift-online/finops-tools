package report

import (
	"fmt"
	"html/template"
	"io"
	"sync"

	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

const executiveSummaryTemplate = "executive-summary"

var (
	esTplOnce sync.Once
	esTpl     *template.Template
	esTplErr  error

	esChartJSOnce sync.Once
	esChartJSSrc  template.JS
	esChartJSErr  error
)

// executiveSummaryTemplateCompiled parses templates/executive-summary.html
// as a standalone template (spec 8: dark theme, tab bar, and KPI grid are a
// different visual language than every other report's shared layout.html).
func executiveSummaryTemplateCompiled() (*template.Template, error) {
	esTplOnce.Do(func() {
		esTpl, esTplErr = template.ParseFS(templateFS, "templates/executive-summary.html")
	})
	return esTpl, esTplErr
}

// executiveSummaryChartJS returns the vendored Chart.js UMD bundle
// (templates/assets/chart.min.js), embedded inline so the report stays one
// self-contained HTML file with no external network dependency.
func executiveSummaryChartJS() (template.JS, error) {
	esChartJSOnce.Do(func() {
		b, err := templateFS.ReadFile("templates/assets/chart.min.js")
		if err != nil {
			esChartJSErr = fmt.Errorf("read vendored chart.js: %w", err)
			return
		}
		esChartJSSrc = template.JS(b)
	})
	return esChartJSSrc, esChartJSErr
}

// KPICardView is one KPI card (spec 7.1, 7.6.2, 7.7).
type KPICardView struct {
	Label   string
	Value   string
	SubText string
	// Class is a kpi-* color class (kpi-good/kpi-warn/kpi-bad/kpi-blue/
	// kpi-purple) applied to Value, or "" for the neutral default.
	Class string
	// SubTextClass colors SubText independently of Class (e.g. the spec
	// 7.6.2 MoM arrow, which can differ in color from the main value).
	SubTextClass string
	// SubText2 is an optional second sub-text line (e.g. spec 7.6.2's
	// "Network: $X (Y% of total)" or "Internal vCPU Hours" lines).
	SubText2      string
	SubText2Class string
	// Tooltip is the spec 8.5 hover-tooltip HTML for the KPI cards that
	// have one; empty for every other card.
	Tooltip template.HTML
}

// BadgeView is a small colored status/severity/method badge.
type BadgeView struct {
	Label string
	Class string
}

// CategoryTableRowView is one Cost-by-Category table row (spec 7.2).
type CategoryTableRowView struct {
	Rank           string // "#1".."#3" for the top 3 positive-delta rows, else ""
	ServiceName    string
	Color          string
	MonthCosts     []string // formatted $ amounts, aligned to the tab's Months
	DeltaFormatted string
	DeltaClass     string // "kpi-bad" (increase) / "kpi-good" (decrease) / ""
}

// ServiceCostRowView is one service+cost line inside a growing-account card.
type ServiceCostRowView struct {
	Service       string
	CostFormatted string
}

// GrowingAccountCardView is one Top-5-Growing-Accounts card (spec 7.3).
type GrowingAccountCardView struct {
	AccountName            string
	ServiceName            string
	LastMonthCostFormatted string
	DeltaFormatted         string // e.g. "▲ $12.3K" / "▼ $4.1K"
	DeltaClass             string // "badge-up" / "badge-down"
	TopServices            []ServiceCostRowView
}

// AnomalyRowView is one merged Cost-Anomalies table row (spec 7.4).
type AnomalyRowView struct {
	Payer           string
	Service         string
	Month           string
	ImpactFormatted string
	Severity        BadgeView
	Method          BadgeView
	Direction       string // "+" or "-"; empty for AWS ML rows
	DirectionClass  string
	ExtraDetail     string // e.g. "Z=+2.3 · +45.2%" for statistical rows
}

// SPTrendView is the Savings Plan Trend section (spec 7.5).
type SPTrendView struct {
	TotalNetSavingsFormatted string
	CoverageUtilChart        chartSpec
	NetSavingsChart          chartSpec
}

// PayerTabView is one payer tab's complete content (spec section 7, tabs 1-4).
type PayerTabView struct {
	ID          string
	Label       string
	AccentClass string // "payer-production" / "payer-staging" / "payer-integration" / ""

	KPICards []KPICardView

	CategoryChart  chartSpec
	CategoryTable  []CategoryTableRowView
	CategoryMonths []string // month display headers for the table/legend

	GrowingAccounts []GrowingAccountCardView

	HasAnomalies bool
	AnomalyRows  []AnomalyRowView
	AlertBanner  []string // only populated for the All Payers tab (spec 7.4)

	HasSPData bool
	SPTrend   SPTrendView
}

// MCSummaryRowView is one Management Cluster Cost Summary table row (spec 7.6.4).
type MCSummaryRowView struct {
	MCName, SCName, Region       string
	MCCostFormatted              string
	CustomerCount, InternalCount int
	VCPUHoursFormatted           string
}

// ClusterDetailRowView is one Customer Cluster Detail table row (spec 7.6.5).
type ClusterDetailRowView struct {
	ClusterID, MCName, Region            string
	Classification                       BadgeView
	Organization, BillingModel           string
	VCPUHoursFormatted, RevenueFormatted string
}

// HeatmapCellView is one populated Regional Performance Heatmap cell (spec
// 8.4). All metric values are carried so the client-side metric toggle can
// re-render without a new query.
type HeatmapCellView struct {
	Month     string
	Empty     bool
	BgColor   string
	TextColor string

	NetRevenue, TotalVCPU, ExtVCPU, IntVCPU                    float64
	Revenue, MCCost, SCCost, NetworkCost, InfraCost, TotalCost float64
	CostPerVCPU, RevenuePerVCPU, MarginPerVCPU                 float64
	ClusterCount, ExtClusterCount                              int

	TooltipMonthLabel string
}

// HeatmapRowView is one region's row in the heatmap (spec 8.4).
type HeatmapRowView struct {
	Region       string
	SparklineSVG template.HTML
	Cells        []HeatmapCellView // aligned to HCPEfficiencyTabView.Months
	Total        HeatmapCellView
}

// HCPEfficiencyTabView is the complete "HCP Cost Efficiency" tab (spec 7.6).
type HCPEfficiencyTabView struct {
	KPICards []KPICardView // the 6 unit-economics cards

	Months      []string
	HeatmapRows []HeatmapRowView

	MCSummaryRows     []MCSummaryRowView
	ClusterDetailRows []ClusterDetailRowView
}

// ClusterGrowthProductCardView is one product-type KPI card (spec 7.7).
type ClusterGrowthProductCardView struct {
	ProductType         string
	CumulativeFormatted string
	DeltaFormatted      string
	DeltaClass          string
	Color               string
}

// ClusterGrowthTabView is the complete "Cluster Growth" tab (spec 7.7).
type ClusterGrowthTabView struct {
	ProductCards []ClusterGrowthProductCardView
	TrendChart   chartSpec
	DeltaChart   chartSpec
}

// ExecutiveSummaryReportView is the template context for
// templates/executive-summary.html.
type ExecutiveSummaryReportView struct {
	GeneratedAt     string
	PeriodEnd       string
	DataSourcesLine string

	Tabs          []PayerTabView // All Payers, Production, Staging, Integration
	HCPEfficiency HCPEfficiencyTabView
	ClusterGrowth ClusterGrowthTabView

	ChartJS      template.JS
	ChartSpecsJS template.JS
}

// payerAccentClass maps a PayerID to its spec 8.1 accent class ("" for the
// neutral All Payers tab).
func payerAccentClass(id execsum.PayerID) string {
	switch id {
	case execsum.PayerProduction:
		return "payer-production"
	case execsum.PayerStaging:
		return "payer-staging"
	case execsum.PayerIntegration:
		return "payer-integration"
	default:
		return ""
	}
}

// NewExecutiveSummaryReportView maps a core execsummary.Report to the
// template context, applying every spec section 7/8 formatting and
// color-class rule.
func NewExecutiveSummaryReportView(r execsum.Report) (ExecutiveSummaryReportView, error) {
	months := windowMonthLabels(r.Window)

	var specs []chartSpec

	tabs := make([]PayerTabView, 0, 1+len(r.Payers))
	allTab, allSpecs := buildPayerTabView(r.AllPayers, months)
	tabs = append(tabs, allTab)
	specs = append(specs, allSpecs...)
	for _, p := range r.Payers {
		tab, tabSpecs := buildPayerTabView(p, months)
		tabs = append(tabs, tab)
		specs = append(specs, tabSpecs...)
	}

	hcpTab, hcpSpecs := buildHCPEfficiencyTabView(r.HCPEfficiency)
	specs = append(specs, hcpSpecs...)

	growthTab, growthSpecs := buildClusterGrowthTabView(r.ClusterGrowth)
	specs = append(specs, growthSpecs...)

	chartJS, err := executiveSummaryChartJS()
	if err != nil {
		return ExecutiveSummaryReportView{}, err
	}
	specsJS, err := chartSpecsJS(specs)
	if err != nil {
		return ExecutiveSummaryReportView{}, fmt.Errorf("marshal chart specs: %w", err)
	}

	periodEnd := ""
	if len(r.Window.Months) > 0 {
		periodEnd = execsum.MonthName(r.Window.Months[len(r.Window.Months)-1])
	}

	return ExecutiveSummaryReportView{
		GeneratedAt:     r.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		PeriodEnd:       periodEnd,
		DataSourcesLine: dataSourcesLine(r.DataSources),
		Tabs:            tabs,
		HCPEfficiency:   hcpTab,
		ClusterGrowth:   growthTab,
		ChartJS:         chartJS,
		ChartSpecsJS:    specsJS,
	}, nil
}

func dataSourcesLine(sources []string) string {
	if len(sources) == 0 {
		return ""
	}
	out := sources[0]
	for _, s := range sources[1:] {
		out += ", " + s
	}
	return out
}

// windowMonthLabels returns the report window's months as YYYY-MM keys,
// ascending -- the shared x-axis/column ordering for every tab and chart.
func windowMonthLabels(w execsum.TimeWindow) []string {
	out := make([]string, len(w.Months))
	for i, m := range w.Months {
		out[i] = execsum.MonthLabel(m)
	}
	return out
}

func lastTwoMonths(months []string) (last, prev string) {
	if len(months) == 0 {
		return "", ""
	}
	last = months[len(months)-1]
	if len(months) >= 2 {
		prev = months[len(months)-2]
	}
	return last, prev
}

// buildPayerTabView assembles one payer tab (spec section 7, tabs 1-4) and
// the chart specs it contributes to the page's shared chart bootstrap.
func buildPayerTabView(p execsum.PayerReport, months []string) (PayerTabView, []chartSpec) {
	var specs []chartSpec

	lastMonth, _ := lastTwoMonths(months)
	tab := PayerTabView{
		ID:          string(p.ID),
		Label:       p.Label,
		AccentClass: payerAccentClass(p.ID),
		KPICards:    buildPayerKPICards(p.KPIs, p.HasSPData, lastMonth),
		HasSPData:   p.HasSPData,
	}

	categoryChart, categoryTable := buildCategorySection(p, months)
	tab.CategoryChart = categoryChart
	tab.CategoryTable = categoryTable
	tab.CategoryMonths = months
	specs = append(specs, categoryChart)

	tab.GrowingAccounts = buildGrowingAccountCards(p.GrowingAccounts)

	tab.AnomalyRows, tab.HasAnomalies = buildAnomalyRows(p)
	if p.ID == execsum.PayerAll {
		tab.AlertBanner = buildAlertBanner(p.MLAnomalies)
	}

	if p.HasSPData {
		spTrend, spSpecs := buildSPTrendView(p)
		tab.SPTrend = spTrend
		specs = append(specs, spSpecs...)
	}

	return tab, specs
}

// RenderExecutiveSummaryHTML renders the executive summary report as HTML to w.
func RenderExecutiveSummaryHTML(w io.Writer, r execsum.Report) error {
	t, err := executiveSummaryTemplateCompiled()
	if err != nil {
		return fmt.Errorf("compile executive-summary template: %w", err)
	}
	view, err := NewExecutiveSummaryReportView(r)
	if err != nil {
		return fmt.Errorf("build executive-summary view: %w", err)
	}
	if err := t.ExecuteTemplate(w, executiveSummaryTemplate, view); err != nil {
		return fmt.Errorf("render executive-summary template: %w", err)
	}
	return nil
}
