// Package execsummary assembles and analyses data for the HCP Executive Summary report.
// All types in this file are pure data structures with no I/O dependencies.
package execsummary

import "time"

// TimeWindow is the reporting window computed by ComputeWindow.
type TimeWindow struct {
	// Start is the first day of the earliest month in the window.
	Start time.Time
	// End is the first day of the last (most recent) month.
	End time.Time
	// Months contains the first day of every month from Start through End, inclusive.
	Months []time.Time
}

// CostRecord is a single monthly cost data point from AWS Cost Explorer
// (golang-executive-summary-spec.md section 3.3.A).
type CostRecord struct {
	// Month is the YYYY-MM key for this record.
	Month string
	// AccountID is the 12-digit AWS account ID (AWS Cost Explorer's LINKED_ACCOUNT).
	AccountID string
	// Cost is the net amortized cost in the account's billing currency.
	Cost float64
	// Service is the AWS service name (empty for account-level aggregates).
	Service string
	// Payer is the payer alias (e.g. "rh-control") used to distinguish multi-payer data.
	Payer string
}

// EnrichedCostRecord is a CostRecord joined with the external mapping CSV's
// Service Name (spec 3.4, 6.7). Accounts absent from the mapping CSV get
// ServiceName = UnmappedServiceName; see AccountTaxonomyMap.ServiceName.
type EnrichedCostRecord struct {
	CostRecord
	// ServiceName is the mapping CSV's category field -- the sole grouping
	// dimension for the Cost-by-Category tab.
	ServiceName string
	// AccountName is the mapping CSV's display name for the account,
	// falling back to the account ID itself when unmapped.
	AccountName string
}

// CategoryMonthRecord aggregates cost by Service Name (the mapping CSV's
// category field) and calendar month (spec 6.7).
type CategoryMonthRecord struct {
	ServiceName string
	// Month is the YYYY-MM key.
	Month string
	Cost  float64
}

// ServiceCostPair is an AWS service name paired with its cost for a given
// account/month (spec 6.8's top_3_services).
type ServiceCostPair struct {
	Service string
	Cost    float64
}

// GrowingAccountRecord identifies an account with significant month-over-
// month cost growth (spec 6.8). The mapping CSV schema has no owner/team
// column, so -- unlike the source Python report -- there is no owner line
// here (spec 3.4).
type GrowingAccountRecord struct {
	AccountID   string
	AccountName string
	ServiceName string
	// LastMonthCost and PrevMonthCost are this account's total cost in the
	// window's two most recent months.
	LastMonthCost float64
	PrevMonthCost float64
	// Delta is LastMonthCost - PrevMonthCost.
	Delta float64
	// TopServices lists the highest-cost services for this account in the
	// last month, descending.
	TopServices []ServiceCostPair
}

// AnomalyRecord identifies a statistically anomalous account-level cost in
// the most recent month of the report window (spec 6.9).
type AnomalyRecord struct {
	AccountID   string
	AccountName string
	ServiceName string
	// Payer is the payer alias this account belongs to.
	Payer string
	// Month is the YYYY-MM key of the anomalous period (the window's last month).
	Month string
	// CurrentCost is the account's cost in Month.
	CurrentCost float64
	// MeanCost is the mean of the account's cost over all prior months in
	// the window (excluding Month).
	MeanCost float64
	// ZScore is (CurrentCost - MeanCost) / stddev(prior months' cost).
	ZScore float64
	// PctChange is (CurrentCost - MeanCost) / MeanCost * 100 (0 if MeanCost is 0).
	PctChange float64
	// Direction is "spike" (ZScore > 0) or "drop" (ZScore < 0).
	Direction string
}

// PayerKPIs holds the computed key performance indicators for one payer or
// the all-payers aggregate (spec 6.10, 6.11), including the spec's exact
// KPI-card color-class thresholds. See ComputePayerKPIs.
type PayerKPIs struct {
	PayerLabel string

	// TotalCostLastMonth and TotalCostPrevMonth are total spend in the
	// window's two most recent months.
	TotalCostLastMonth float64
	TotalCostPrevMonth float64
	// MoMPct is the month-over-month change as a percentage (spec 6.10).
	MoMPct float64
	// MoMClass is "bad"/"warn"/"good" per spec 6.10's MoM thresholds.
	MoMClass string

	// ClusterCount is the cumulative active cluster count in the last month.
	ClusterCount int
	// ClusterDelta is cluster_count[month] - cluster_count[month-1] (spec
	// 6.11); nil when no prior-month snapshot exists.
	ClusterDelta *int

	// HCPCost is the subset of spend attributable to HCP Management/Service
	// Cluster accounts.
	HCPCost float64
	// HCPUnitCost is HCPCost / ClusterCount; nil when ClusterCount is 0.
	HCPUnitCost *float64

	// SPCoverageAvg and SPUtilizationAvg are the simple mean across payers
	// over the window (spec 3.3); nil when no SP data was available.
	SPCoverageAvg      *float64
	SPUtilizationAvg   *float64
	SPCoverageClass    string // "good" if SPCoverageAvg >= 70 else "warn"
	SPUtilizationClass string // "good" if SPUtilizationAvg >= 90 else "warn"

	// TotalNetSavings and TotalOnDemandEquiv are summed across payers over
	// the window (spec 3.3).
	TotalNetSavings    float64
	TotalOnDemandEquiv float64
	// SavingsRate is (TotalNetSavings/TotalOnDemandEquiv)*100; nil if
	// TotalOnDemandEquiv is 0.
	SavingsRate      *float64
	SavingsRateClass string // "warn" if SavingsRate < 20 else "good"

	// AnomalyCount is the number of statistical anomalies detected for this
	// payer/window.
	AnomalyCount int
	// AnomalyClass is "bad" if AnomalyCount > 5, "warn" if > 0, else "good".
	AnomalyClass string
}
