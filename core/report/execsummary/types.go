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

// CostRecord is a single monthly cost data point from AWS Cost Explorer.
type CostRecord struct {
	// Month is the YYYY-MM key for this record.
	Month string
	// AccountID is the 12-digit AWS account ID.
	AccountID string
	// Cost is the net amortized cost in the account's billing currency.
	Cost float64
	// Service is the AWS service name (empty for account-level aggregates).
	Service string
	// Payer is the payer alias (e.g. "rh-control") used to distinguish multi-payer data.
	Payer string
}

// AccountMapping maps an AWS account ID to business metadata from CLOUDOSCOPE_DB.
type AccountMapping struct {
	// AccountID is the 12-digit AWS account ID.
	AccountID string
	// RefinedCategory is the business category (e.g. "HCP", "Tooling", "Shared").
	RefinedCategory string
	// SubType provides a finer-grained classification within RefinedCategory.
	SubType string
	// OwnerTeam is the team responsible for the account.
	OwnerTeam string
	// AccountName is the human-readable AWS account name.
	AccountName string
}

// EnrichedCostRecord is a CostRecord joined with AccountMapping metadata.
// Accounts not present in the mapping have RefinedCategory = "Unmapped".
type EnrichedCostRecord struct {
	CostRecord
	RefinedCategory string
	SubType         string
	OwnerTeam       string
	AccountName     string
}

// CategoryMonthRecord aggregates cost by refined category and calendar month.
type CategoryMonthRecord struct {
	RefinedCategory string
	// Month is the YYYY-MM key.
	Month string
	Cost  float64
}

// ServiceCostPair is a service name paired with its cost for a given account/month.
type ServiceCostPair struct {
	Service string
	Cost    float64
}

// GrowingAccountRecord identifies an account with significant month-over-month growth.
type GrowingAccountRecord struct {
	AccountID     string
	AccountName   string
	Category      string
	OwnerTeam     string
	LastMonthCost float64
	PrevMonthCost float64
	// Delta is LastMonthCost − PrevMonthCost.
	Delta float64
	// TopServices lists the highest-cost services for this account in the last month.
	TopServices []ServiceCostPair
}

// AnomalyRecord identifies a statistically anomalous cost for an account/service/month.
type AnomalyRecord struct {
	AccountID   string
	AccountName string
	Category    string
	Service     string
	// Month is the YYYY-MM key of the anomalous period.
	Month       string
	CurrentCost float64
	MeanCost    float64
	// ZScore is the number of standard deviations above the historical mean.
	ZScore      float64
}

// PayerKPIs holds the computed key performance indicators for one payer or the
// all-payers aggregate. A nil pointer field means data was unavailable.
type PayerKPIs struct {
	PayerLabel string
	// TotalCostLastMonth is total spend in the most recent full month.
	TotalCostLastMonth float64
	// TotalCostPrevMonth is total spend in the prior month.
	TotalCostPrevMonth float64
	// MoMPct is the month-over-month change as a percentage (positive = increase).
	MoMPct float64
	// HCPCost is the subset of spend attributable to HCP accounts.
	HCPCost float64
	// ClusterCount is the number of active clusters in the last month.
	ClusterCount int
	// ClusterDelta is the change in cluster count from the prior month.
	ClusterDelta int
	// HCPUnitCost is HCPCost divided by ClusterCount (nil when ClusterCount is zero).
	HCPUnitCost *float64
	// SPCoverageAvg is the average Savings Plans coverage percentage over the window.
	SPCoverageAvg *float64
	// SPUtilizationAvg is the average Savings Plans utilization percentage over the window.
	SPUtilizationAvg *float64
}
