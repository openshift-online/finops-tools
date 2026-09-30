package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

func floatP(v float64) *float64 { return &v }

func fixtureReport() execsum.Report {
	months := []string{"2026-03", "2026-04", "2026-05"}
	timeMonths := make([]time.Time, len(months))
	for i, m := range months {
		t, _ := time.Parse("2006-01", m)
		timeMonths[i] = t
	}
	window := execsum.TimeWindow{Start: timeMonths[0], End: timeMonths[len(timeMonths)-1], Months: timeMonths}

	makePayer := func(id execsum.PayerID, label string, hasSP bool) execsum.PayerReport {
		p := execsum.PayerReport{
			ID:    id,
			Label: label,
			KPIs: execsum.PayerKPIs{
				PayerLabel:         label,
				TotalCostLastMonth: 120000,
				TotalCostPrevMonth: 100000,
				MoMPct:             20,
				MoMClass:           "bad",
				ClusterCount:       150,
				ClusterDelta:       intPtr(5),
				HCPCost:            30000,
				HCPUnitCost:        floatP(200.5),
				AnomalyCount:       2,
				AnomalyClass:       "warn",
			},
			CategoryMonths: []execsum.CategoryMonthRecord{
				{ServiceName: "Compute", Month: "2026-04", Cost: 1000},
				{ServiceName: "Compute", Month: "2026-05", Cost: 1500},
				{ServiceName: "Storage", Month: "2026-04", Cost: 5000},
				{ServiceName: "Storage", Month: "2026-05", Cost: 4000},
			},
			GrowingAccounts: []execsum.GrowingAccountRecord{
				{
					AccountID: "111111111111", AccountName: "Acme", ServiceName: "Platform",
					LastMonthCost: 20000, PrevMonthCost: 15000, Delta: 5000,
					TopServices: []execsum.ServiceCostPair{{Service: "EC2", Cost: 12000}, {Service: "S3", Cost: 3000}},
				},
			},
			MLAnomalies: []execsum.MLAnomalyRecord{
				{Payer: "rh-control", Service: "EC2", TotalImpact: 60000, StartDate: "2026-05-01"},
				{Payer: "rh-control", Service: "RDS", TotalImpact: 12000, StartDate: "2026-05-03"},
			},
			StatisticalAnomalies: []execsum.AnomalyRecord{
				{AccountID: "222222222222", AccountName: "Beta", ServiceName: "Compute", Payer: "rh-control", Month: "2026-05", CurrentCost: 9000, MeanCost: 3000, ZScore: 3.2, PctChange: 200, Direction: "spike"},
			},
		}
		p.CategoryOrder = execsum.CategoryOrderByTotalCost(p.CategoryMonths)
		if hasSP {
			p.HasSPData = true
			p.SPMonths = []execsum.SPMonthPoint{
				{Month: "2026-03", CoveragePercentage: floatP(60), UtilizationPercentage: floatP(80), NetSavings: 1000},
				{Month: "2026-04", CoveragePercentage: floatP(70), UtilizationPercentage: floatP(90), NetSavings: 1200},
				{Month: "2026-05", CoveragePercentage: floatP(75), UtilizationPercentage: floatP(95), NetSavings: 1500},
			}
			p.SPNetSavingsWindow = 3700
			p.KPIs.SPCoverageAvg = floatP(75)
			p.KPIs.SPUtilizationAvg = floatP(95)
			p.KPIs.SPCoverageClass = "good"
			p.KPIs.SPUtilizationClass = "good"
			p.KPIs.TotalNetSavings = 3700
			p.KPIs.TotalOnDemandEquiv = 10000
			p.KPIs.SavingsRate = floatP(37)
			p.KPIs.SavingsRateClass = "good"
		}
		return p
	}

	allPayers := makePayer(execsum.PayerAll, "All Payers", true)
	production := makePayer(execsum.PayerProduction, "Production", true)
	staging := makePayer(execsum.PayerStaging, "Staging", false)
	integration := makePayer(execsum.PayerIntegration, "Integration", false)

	hcpEff := execsum.HCPEfficiencyReport{
		Months: months,
		Regions: []execsum.RegionMonthMetrics{
			{Region: "us-east-1", Month: "2026-03", NetRevenue: 5000, TotalCost: 2000, Revenue: 7000, VCPUHours: 1000, ExtVCPUHours: 900, IntVCPUHours: 100, CostPerVCPU: 2.2, RevenuePerVCPU: 7.8, MarginPerVCPU: 5.6, ClusterCount: 10, ExtClusterCount: 8, MCCost: 1500, SCCost: 500},
			{Region: "us-east-1", Month: "2026-04", NetRevenue: 6000, TotalCost: 2500, Revenue: 8500, VCPUHours: 1100, ExtVCPUHours: 1000, IntVCPUHours: 100, CostPerVCPU: 2.5, RevenuePerVCPU: 8.5, MarginPerVCPU: 6.0, ClusterCount: 11, ExtClusterCount: 9},
			{Region: "us-east-1", Month: "2026-05", NetRevenue: 7000, TotalCost: 3000, Revenue: 10000, VCPUHours: 1200, ExtVCPUHours: 1050, IntVCPUHours: 150, CostPerVCPU: 2.9, RevenuePerVCPU: 9.5, MarginPerVCPU: 6.7, ClusterCount: 12, ExtClusterCount: 10},
			{Region: "eu-west-1", Month: "2026-05", NetRevenue: -1000, TotalCost: 4000, Revenue: 3000, VCPUHours: 500, ExtVCPUHours: 400, IntVCPUHours: 100, CostPerVCPU: 10, RevenuePerVCPU: 7.5, MarginPerVCPU: -2.5, ClusterCount: 4, ExtClusterCount: 3},
		},
		VCPUEfficiency: []execsum.VCPUEfficiencyRecord{
			{Classification: "external", Month: "2026-04", VCPUHours: 2_000_000, AllocatedCost: 40000, CostPerVCPU: 0.02, RevenuePerVCPU: 0.04275, NetMarginPerVCPU: 0.02275},
			{Classification: "external", Month: "2026-05", VCPUHours: 2_200_000, AllocatedCost: 48000, CostPerVCPU: 0.0218, RevenuePerVCPU: 0.04275, NetMarginPerVCPU: 0.02095},
			{Classification: "internal", Month: "2026-04", VCPUHours: 300_000, AllocatedCost: 5000, CostPerVCPU: 0.0167},
			{Classification: "internal", Month: "2026-05", VCPUHours: 350_000, AllocatedCost: 6000, CostPerVCPU: 0.0171},
		},
		MCSummaryLatestMonth: []execsum.MCSummaryRecord{
			{MCName: "mc-a", MCRegion: "us-east-1", SCName: "sc-a", Month: "2026-05", MCCost: 3000, CustomerCount: 12, InternalCount: 2, VCPUHours: 1200},
		},
		ClusterDetailLatestMonth: []execsum.ClusterDetailRecord{
			{ClusterID: "c1", MCName: "mc-a", Region: "us-east-1", Classification: "external", Organization: "org-a", BillingModel: "standard", VCPUHours: 500, Revenue: 21.375},
		},
	}

	growth := execsum.ClusterGrowthTabReport{
		Months:       months,
		ProductTypes: []string{"ROSA HCP", "OSD"},
		Points: []execsum.ClusterGrowthProductPoint{
			{ProductType: "ROSA HCP", Month: "2026-03", Cumulative: 100, NewClusters: 5},
			{ProductType: "ROSA HCP", Month: "2026-04", Cumulative: 108, NewClusters: 8},
			{ProductType: "ROSA HCP", Month: "2026-05", Cumulative: 115, NewClusters: 7},
			{ProductType: "OSD", Month: "2026-03", Cumulative: 50, NewClusters: 1},
			{ProductType: "OSD", Month: "2026-04", Cumulative: 50, NewClusters: 0},
			{ProductType: "OSD", Month: "2026-05", Cumulative: 52, NewClusters: 2},
		},
	}

	return execsum.Report{
		GeneratedAt:   time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		Window:        window,
		DataSources:   []string{"AWS Cost Explorer + Mapping CSV", "OCM_MAPPING"},
		AllPayers:     allPayers,
		Payers:        []execsum.PayerReport{production, staging, integration},
		HCPEfficiency: hcpEff,
		ClusterGrowth: growth,
	}
}

func TestRenderExecutiveSummaryHTML_Smoke(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderExecutiveSummaryHTML(&buf, fixtureReport()); err != nil {
		t.Fatalf("RenderExecutiveSummaryHTML error: %v", err)
	}
	html := buf.String()

	wantSubstrings := []string{
		"HCP Executive Summary",
		"All Payers", "Production", "Staging", "Integration",
		"HCP Cost Efficiency", "Cluster Growth",
		"CALCULATED REVENUE",
		"Acme",                   // growing account
		"us-east-1", "eu-west-1", // heatmap regions
		"ROSA HCP", "OSD", // cluster growth products
		"category-chart-all",
		"cluster-growth-trend-chart",
		"High-Severity Cost Anomalies Detected", // All Payers alert banner (ML impact 60000 > 10000)
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing expected substring %q", want)
		}
	}

	// Staging/Integration have no SP data -- their SP Trend section and
	// SP-related KPI cards must be entirely omitted.
	stagingIdx := strings.Index(html, `id="tab-staging"`)
	integrationIdx := strings.Index(html, `id="tab-integration"`)
	hcpIdx := strings.Index(html, `id="tab-hcp-efficiency"`)
	if stagingIdx < 0 || integrationIdx < 0 || hcpIdx < 0 {
		t.Fatalf("could not locate tab panels in rendered HTML")
	}
	stagingSection := html[stagingIdx:integrationIdx]
	if strings.Contains(stagingSection, "Savings Plan Trend") {
		t.Error("staging tab (no SP data) should not render the Savings Plan Trend section")
	}
	if strings.Contains(stagingSection, "SP Coverage") {
		t.Error("staging tab (no SP data) should not render SP-related KPI cards")
	}
}

func TestBuildPayerKPICards_OmitsSPCardsWhenNoData(t *testing.T) {
	k := execsum.PayerKPIs{ClusterCount: 10}
	cards := buildPayerKPICards(k, false, "May 2026")
	for _, c := range cards {
		if c.Label == "SP Coverage" || c.Label == "SP Utilization" || c.Label == "SP Net Savings YTD" || c.Label == "Savings Rate" {
			t.Errorf("unexpected SP card %q when hasSPData=false", c.Label)
		}
	}
	if len(cards) != 5 {
		t.Errorf("len(cards) = %d, want 5 (9 total - 4 SP cards)", len(cards))
	}
}

func TestBuildPayerKPICards_IncludesSPCardsWhenDataPresent(t *testing.T) {
	k := execsum.PayerKPIs{
		ClusterCount:     10,
		SPCoverageAvg:    floatP(80),
		SPUtilizationAvg: floatP(95),
		SavingsRate:      floatP(25),
	}
	cards := buildPayerKPICards(k, true, "May 2026")
	if len(cards) != 9 {
		t.Fatalf("len(cards) = %d, want 9", len(cards))
	}
	if cards[2].Label != "SP Coverage" || cards[2].Value != "80.0%" {
		t.Errorf("cards[2] = %+v, want SP Coverage 80.0%%", cards[2])
	}
}

func TestBuildPayerKPICards_HCPUnitCostDashWhenNil(t *testing.T) {
	cards := buildPayerKPICards(execsum.PayerKPIs{}, false, "May 2026")
	for _, c := range cards {
		if c.Label == "HCP Unit Cost" {
			if c.Value != "—" {
				t.Errorf("HCP Unit Cost = %q, want em dash when nil", c.Value)
			}
			return
		}
	}
	t.Error("HCP Unit Cost card not found")
}

func TestBuildPayerKPICards_ClusterDeltaSubText(t *testing.T) {
	cards := buildPayerKPICards(execsum.PayerKPIs{ClusterDelta: intPtr(-3)}, false, "May 2026")
	for _, c := range cards {
		if c.Label == "Cluster Count" {
			if c.SubText != "MoM delta: -3" {
				t.Errorf("SubText = %q, want 'MoM delta: -3'", c.SubText)
			}
			if c.Class != "kpi-warn" {
				t.Errorf("Class = %q, want kpi-warn for negative delta", c.Class)
			}
			return
		}
	}
	t.Error("Cluster Count card not found")
}

func TestHeatmapCellColor_PositiveAndNegativeScaleIndependently(t *testing.T) {
	bgFull, _ := heatmapCellColor(100, 100, 50) // full positive ratio -> pure green
	if bgFull != "#63993d" {
		t.Errorf("full-ratio positive color = %q, want #63993d (pure green)", bgFull)
	}
	bgZero, textZero := heatmapCellColor(0, 100, 50)
	if bgZero != "#ffffff" {
		t.Errorf("zero value color = %q, want #ffffff (white)", bgZero)
	}
	if textZero != "#1a1a1a" {
		t.Errorf("zero value text = %q, want near-black", textZero)
	}
	bgNeg, _ := heatmapCellColor(-50, 100, 50) // full negative ratio -> pure red
	if bgNeg != "#ee0000" {
		t.Errorf("full-ratio negative color = %q, want #ee0000 (pure red)", bgNeg)
	}
}

func TestHeatmapCellColor_TextColorContrastThreshold(t *testing.T) {
	// ratio just above 0.55 -> white text; just below -> near-black.
	_, textHigh := heatmapCellColor(60, 100, 0) // ratio 0.6
	if textHigh != "#ffffff" {
		t.Errorf("text at ratio 0.6 = %q, want #ffffff", textHigh)
	}
	_, textLow := heatmapCellColor(50, 100, 0) // ratio 0.5
	if textLow != "#1a1a1a" {
		t.Errorf("text at ratio 0.5 = %q, want #1a1a1a", textLow)
	}
}

func TestSparklineSVG_FlatLineWhenAllValuesEqual(t *testing.T) {
	svg := sparklineSVG([]float64{5, 5, 5, 5, 5})
	if !strings.Contains(string(svg), "10.0") { // mid-height (h/2 = 10) appears in every point
		t.Errorf("flat sparkline should position every point at mid-height: %s", svg)
	}
}

func TestSparklineSVG_EmptyValues(t *testing.T) {
	if got := sparklineSVG(nil); got != "" {
		t.Errorf("sparklineSVG(nil) = %q, want empty", got)
	}
}

func TestBuildAnomalyRows_MergesMLAndStatisticalWithCorrectSeverity(t *testing.T) {
	p := execsum.PayerReport{
		MLAnomalies: []execsum.MLAnomalyRecord{
			{Payer: "rh-control", Service: "EC2", TotalImpact: 60000},
			{Payer: "rh-control", Service: "S3", TotalImpact: 5000},
		},
		StatisticalAnomalies: []execsum.AnomalyRecord{
			{ServiceName: "RDS", CurrentCost: 20000, MeanCost: 1000, ZScore: 4, Direction: "spike"},
		},
	}
	rows, has := buildAnomalyRows(p)
	if !has {
		t.Fatal("has = false, want true")
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0].Service != "EC2" || rows[0].Severity.Label != "Critical" {
		t.Errorf("rows[0] = %+v, want EC2/Critical (impact 60000 > 50000)", rows[0])
	}
	if rows[1].Service != "S3" || rows[1].Severity.Label != "Medium" {
		t.Errorf("rows[1] = %+v, want S3/Medium (impact 5000 <= 10000)", rows[1])
	}
	if rows[2].Service != "RDS" || rows[2].Direction != "+" {
		t.Errorf("rows[2] = %+v, want RDS/+ (spike)", rows[2])
	}
}

func TestBuildAnomalyRows_EmptyWhenNoAnomalies(t *testing.T) {
	rows, has := buildAnomalyRows(execsum.PayerReport{})
	if has || len(rows) != 0 {
		t.Errorf("rows=%v has=%v, want empty/false", rows, has)
	}
}

func TestBuildAlertBanner_FiltersByImpactThresholdAndCapsAtThree(t *testing.T) {
	ml := []execsum.MLAnomalyRecord{
		{Service: "A", TotalImpact: 11000, Payer: "p1"},
		{Service: "B", TotalImpact: 9000, Payer: "p1"}, // below threshold, excluded
		{Service: "C", TotalImpact: 50000, Payer: "p2"},
		{Service: "D", TotalImpact: 20000, Payer: "p3"},
		{Service: "E", TotalImpact: 15000, Payer: "p4"},
	}
	lines := buildAlertBanner(ml)
	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
	if !strings.Contains(lines[0], "C —") {
		t.Errorf("lines[0] = %q, want highest-impact anomaly (C) first", lines[0])
	}
}

func TestBuildCategorySection_TableSortedByAbsDeltaDescendingWithRankBadges(t *testing.T) {
	p := execsum.PayerReport{
		CategoryMonths: []execsum.CategoryMonthRecord{
			{ServiceName: "Big Increase", Month: "2026-04", Cost: 100},
			{ServiceName: "Big Increase", Month: "2026-05", Cost: 1000}, // +900
			{ServiceName: "Big Decrease", Month: "2026-04", Cost: 1000},
			{ServiceName: "Big Decrease", Month: "2026-05", Cost: 100}, // -900 (same abs delta, tiebreak by original order)
			{ServiceName: "Flat", Month: "2026-04", Cost: 50},
			{ServiceName: "Flat", Month: "2026-05", Cost: 55}, // +5, small
		},
	}
	p.CategoryOrder = execsum.CategoryOrderByTotalCost(p.CategoryMonths)
	_, rows := buildCategorySection(p, []string{"2026-04", "2026-05"})
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[2].ServiceName != "Flat" {
		t.Errorf("rows[2] = %+v, want Flat (smallest abs delta) last", rows[2])
	}
	// "Big Increase" and "Big Decrease" tie on |delta|=900 (and on total
	// cost, 1100 each), so the stable sort preserves their
	// CategoryOrderByTotalCost tiebreak order (alphabetical: "Big
	// Decrease" before "Big Increase"). Rank badges go on positive-delta
	// rows only: "Big Increase" (+900) and "Flat" (+5); "Big Decrease"
	// (-900) is not ranked.
	if rows[0].ServiceName != "Big Decrease" || rows[0].Rank != "" {
		t.Errorf("rows[0] = %+v, want Big Decrease unranked (negative delta)", rows[0])
	}
	if rows[1].ServiceName != "Big Increase" || rows[1].Rank != "#1" {
		t.Errorf("rows[1] = %+v, want Big Increase ranked #1", rows[1])
	}
	if rows[2].Rank != "#2" {
		t.Errorf("rows[2] = %+v, want Flat ranked #2", rows[2])
	}
}

func TestBuildClusterGrowthTabView_ProductOrderAndDeltas(t *testing.T) {
	r := execsum.ClusterGrowthTabReport{
		Months: []string{"2026-04", "2026-05"},
		Points: []execsum.ClusterGrowthProductPoint{
			{ProductType: "OSD", Month: "2026-04", Cumulative: 10, NewClusters: 1},
			{ProductType: "OSD", Month: "2026-05", Cumulative: 12, NewClusters: 2},
			{ProductType: "ROSA HCP", Month: "2026-04", Cumulative: 100, NewClusters: 5},
			{ProductType: "ROSA HCP", Month: "2026-05", Cumulative: 90, NewClusters: -10}, // shrank
		},
	}
	tab, specs := buildClusterGrowthTabView(r)
	if len(tab.ProductCards) != 2 {
		t.Fatalf("len(ProductCards) = %d, want 2", len(tab.ProductCards))
	}
	// ROSA HCP sorts before OSD per canonical product order.
	if tab.ProductCards[0].ProductType != "ROSA HCP" {
		t.Errorf("ProductCards[0].ProductType = %q, want ROSA HCP first", tab.ProductCards[0].ProductType)
	}
	if tab.ProductCards[0].DeltaFormatted != "-10" || tab.ProductCards[0].DeltaClass != "kpi-warn" {
		t.Errorf("ROSA HCP card = %+v, want delta -10/kpi-warn", tab.ProductCards[0])
	}
	if tab.ProductCards[1].DeltaFormatted != "+2" || tab.ProductCards[1].DeltaClass != "kpi-good" {
		t.Errorf("OSD card = %+v, want delta +2/kpi-good", tab.ProductCards[1])
	}
	if len(specs) != 2 {
		t.Errorf("len(specs) = %d, want 2 (trend + delta charts)", len(specs))
	}
	if !specs[0].SpanGaps || specs[0].Type != "line" {
		t.Errorf("trend chart spec = %+v, want line chart with spanGaps", specs[0])
	}
	if !specs[1].Stacked || specs[1].Type != "bar" {
		t.Errorf("delta chart spec = %+v, want stacked bar", specs[1])
	}
}
