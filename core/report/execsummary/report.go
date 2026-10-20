package execsummary

import (
	"sort"
	"time"
)

// PayerID identifies one of the report's tabs (spec section 7): the three
// AWS payer tabs plus the aggregated "All Payers" tab. The HCP Cost
// Efficiency and Cluster Growth tabs are global (not per-payer) and have no
// PayerID.
type PayerID string

// Canonical payer tab IDs, in display order (spec section 7, tabs 1-4).
const (
	PayerAll         PayerID = "all"
	PayerProduction  PayerID = "production"
	PayerStaging     PayerID = "staging"
	PayerIntegration PayerID = "integration"
)

// MLAnomalyRecord is one AWS Cost Anomaly Detection finding
// (golang-executive-summary-spec.md sections 3.3.D / 7.4), tagged with the
// payer alias it was detected on.
type MLAnomalyRecord struct {
	Payer         string
	Service       string
	TotalImpact   float64
	ActualSpend   float64
	ExpectedSpend float64
	StartDate     string
	EndDate       string
}

// SPMonthPoint is one month's combined (already averaged/summed across
// payers per spec 3.3) Savings Plans coverage/utilization/net-savings
// figures, for one payer tab's SP Trend section (spec 7.5).
type SPMonthPoint struct {
	Month                 string
	CoveragePercentage    *float64
	UtilizationPercentage *float64
	NetSavings            float64
}

// PayerReport is the complete computed content for one tab (spec section 7,
// tabs 1-4): KPI cards, cost-by-category, top-5 growing accounts,
// anomalies, and the Savings Plan trend.
type PayerReport struct {
	ID    PayerID
	Label string // "All Payers", "Production", "Staging", "Integration"

	KPIs PayerKPIs

	// CategoryMonths is spec 6.7's category x month cost for every month
	// in the window. CategoryOrder lists the same categories sorted by
	// total cost across the window, descending (spec 7.2's chart-stacking
	// order) -- see CategoryOrderByTotalCost.
	CategoryMonths []CategoryMonthRecord
	CategoryOrder  []string

	GrowingAccounts []GrowingAccountRecord // spec 6.8, top 5

	MLAnomalies          []MLAnomalyRecord // spec 3.3.D/7.4, top 10 by impact desc
	StatisticalAnomalies []AnomalyRecord   // spec 6.9/7.4, top 10 of the capped 15

	// HasSPData is false when this payer has no coverage/utilization data
	// at all; callers must omit the 4 SP-related KPI cards and the SP
	// Trend section entirely in that case (spec 7.1, 7.5).
	HasSPData          bool
	SPMonths           []SPMonthPoint
	SPNetSavingsWindow float64 // spec 7.5 header line: sum of net_savings across the window
}

// ClusterDetailRecord is one row of the spec 7.6.5 Customer Cluster Detail
// table (latest month only, top 100 by vCPU hours).
type ClusterDetailRecord struct {
	ClusterID      string
	MCName         string
	Region         string
	Classification string
	Organization   string
	BillingModel   string
	VCPUHours      float64
	Revenue        float64
}

// ClusterDetailInput is the minimal per-cluster data needed to build one
// month's ClusterDetailRecord rows (spec 7.6.5). Build it from
// hcphierarchy.HierarchyRow -- this package stays dependency-free.
type ClusterDetailInput struct {
	ClusterID      string
	MCName         string
	Region         string
	Classification string
	Organization   string
	BillingModel   string
}

// BuildClusterDetailRecords implements spec 7.6.5: join per-cluster
// metadata with one month's vCPU-hours, compute each cluster's revenue
// (spec 6.1), and return the top `limit` rows by vCPU hours, descending.
func BuildClusterDetailRecords(clusters []ClusterDetailInput, vcpu []ClusterMonthVCPU, month string, limit int) []ClusterDetailRecord {
	clusterByID := make(map[string]ClusterDetailInput, len(clusters))
	for _, c := range clusters {
		clusterByID[c.ClusterID] = c
	}

	out := make([]ClusterDetailRecord, 0, len(vcpu))
	for _, v := range vcpu {
		if v.Month != month {
			continue
		}
		c, ok := clusterByID[v.ClusterID]
		if !ok {
			continue
		}
		out = append(out, ClusterDetailRecord{
			ClusterID:      c.ClusterID,
			MCName:         c.MCName,
			Region:         c.Region,
			Classification: c.Classification,
			Organization:   c.Organization,
			BillingModel:   c.BillingModel,
			VCPUHours:      v.VCPUHours,
			Revenue:        Revenue(v.VCPUHours, c.Classification),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].VCPUHours != out[j].VCPUHours {
			return out[i].VCPUHours > out[j].VCPUHours
		}
		return out[i].ClusterID < out[j].ClusterID // deterministic tiebreak
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// HCPEfficiencyReport is the complete computed content for the "HCP Cost
// Efficiency" tab (spec section 7.6): merged Fleet Profitability heatmap +
// unit-economics KPIs.
type HCPEfficiencyReport struct {
	Months  []string             // ascending YYYY-MM keys
	Regions []RegionMonthMetrics // every populated (region, month) cell across the window (spec 6.4 exclusion already applied)

	VCPUEfficiency []VCPUEfficiencyRecord // spec 6.6, per (classification, month)

	MCSummaryLatestMonth     []MCSummaryRecord     // spec 7.6.4, latest month only, top 25 by MCCost desc
	ClusterDetailLatestMonth []ClusterDetailRecord // spec 7.6.5, latest month only, top 100 by VCPUHours desc
}

// Report is the complete computed input to the executive-summary HTML
// renderer (spec section 7): one PayerReport per AWS payer tab plus the
// aggregated All Payers tab, and the two global tabs.
type Report struct {
	GeneratedAt time.Time
	Window      TimeWindow
	// DataSources lists the data sources used for this run, for the footer
	// (spec 8.6), e.g. "AWS Cost Explorer + Mapping CSV", "OCM_MAPPING".
	DataSources []string

	AllPayers PayerReport
	Payers    []PayerReport // Production, Staging, Integration, in that order

	HCPEfficiency HCPEfficiencyReport
	ClusterGrowth ClusterGrowthTabReport
}

// CategoryOrderByTotalCost implements spec 7.2's chart-stacking order:
// every distinct Service Name in categoryMonths, sorted by total cost
// summed across the whole window, descending (highest-total-cost category
// first). Ties break alphabetically for determinism.
func CategoryOrderByTotalCost(categoryMonths []CategoryMonthRecord) []string {
	totals := make(map[string]float64)
	var order []string
	seen := make(map[string]bool)
	for _, r := range categoryMonths {
		if !seen[r.ServiceName] {
			seen[r.ServiceName] = true
			order = append(order, r.ServiceName)
		}
		totals[r.ServiceName] += r.Cost
	}
	sort.Slice(order, func(i, j int) bool {
		if totals[order[i]] != totals[order[j]] {
			return totals[order[i]] > totals[order[j]]
		}
		return order[i] < order[j]
	})
	return order
}
