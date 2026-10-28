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
