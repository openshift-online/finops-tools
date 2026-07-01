package snapshot

import "strings"

const (
	ebsStandardUSDPerGiBMonth = 0.05
	ebsArchiveUSDPerGiBMonth  = 0.0125
	rdsSnapshotUSDPerGiBMonth = 0.095
)

// EstimateMonthlyCost returns an approximate monthly storage cost in USD.
func EstimateMonthlyCost(kind Kind, sizeGiB float64, storageTier, _ string) float64 {
	if sizeGiB <= 0 {
		return 0
	}
	switch kind {
	case KindEBSSnapshot:
		if strings.EqualFold(strings.TrimSpace(storageTier), "archive") {
			return sizeGiB * ebsArchiveUSDPerGiBMonth
		}
		return sizeGiB * ebsStandardUSDPerGiBMonth
	case KindRDSSnapshot, KindRDSClusterSnapshot:
		return sizeGiB * rdsSnapshotUSDPerGiBMonth
	default:
		return 0
	}
}

// RDSRegionContext holds regional inputs for RDS backup free-tier pricing.
type RDSRegionContext struct {
	FreePoolGiB       float64
	TotalBackupGiB    float64
	BillableBackupGiB float64
	LiveInstanceIDs   map[string]struct{}
	LiveClusterIDs    map[string]struct{}
}

// RDSMonthlyBackupRunRateUSD estimates gross monthly RDS backup storage spend for
// constant regional excess backup footprint (GiB-months × rate). Compare net
// spend to Cost Explorer usage type RDS:ChargedBackupUsage.
func RDSMonthlyBackupRunRateUSD(excessGiB float64) float64 {
	if excessGiB <= 0 {
		return 0
	}
	return excessGiB * rdsSnapshotUSDPerGiBMonth
}

// ApplyRDSRegionalCosts updates RDS snapshot records using the regional free
// storage allowance (100% of provisioned DB storage). Billable excess (manual
// snapshots and automated snapshots from deleted instances) is allocated across
// billable snapshots proportionally by allocated size. When automated backups
// from live instances dominate regional excess, only billable GiB can be removed
// by deleting listed snapshots, so allocatable excess is capped at billable
// backup size (regional run-rate summaries still use full excess).
func ApplyRDSRegionalCosts(records []Record, ctx RDSRegionContext) {
	excessGiB := ctx.TotalBackupGiB - ctx.FreePoolGiB
	if excessGiB <= 0 || ctx.TotalBackupGiB <= 0 {
		for i := range records {
			if !isRDSKind(records[i].Kind) {
				continue
			}
			records[i].EstimatedMonthlyCostUSD = 0
			records[i].CostBasis = CostBasisRDSWithinFreeTier
		}
		return
	}

	billableGiB := ctx.BillableBackupGiB
	if billableGiB <= 0 {
		for i := range records {
			if !isRDSKind(records[i].Kind) {
				continue
			}
			records[i].EstimatedMonthlyCostUSD = 0
			records[i].CostBasis = CostBasisRDSWithinFreeTier
		}
		return
	}

	billableExcessGiB := excessGiB
	if billableGiB < excessGiB {
		billableExcessGiB = billableGiB
	}
	billableExcessCost := RDSMonthlyBackupRunRateUSD(billableExcessGiB)

	for i := range records {
		if !isRDSKind(records[i].Kind) {
			continue
		}
		if !rdsSnapshotIsBillable(records[i], ctx) {
			records[i].EstimatedMonthlyCostUSD = 0
			records[i].CostBasis = CostBasisRDSWithinFreeTier
			continue
		}
		share := records[i].SizeGiB / billableGiB
		records[i].EstimatedMonthlyCostUSD = billableExcessCost * share
		records[i].CostBasis = CostBasisRDSRegionalExcess
	}
}

func rdsSnapshotIsBillable(rec Record, ctx RDSRegionContext) bool {
	if strings.EqualFold(strings.TrimSpace(rec.SnapshotType), "automated") && rdsSourceIsLive(rec, ctx) {
		return false
	}
	return true
}

func rdsSourceIsLive(rec Record, ctx RDSRegionContext) bool {
	sourceID := strings.TrimSpace(rec.SourceResourceID)
	if sourceID == "" {
		return false
	}
	if rec.Kind == KindRDSClusterSnapshot {
		_, ok := ctx.LiveClusterIDs[sourceID]
		return ok
	}
	_, ok := ctx.LiveInstanceIDs[sourceID]
	return ok
}

// ApplyRDSLegacyCosts assigns per-snapshot full-rate estimates when regional
// context is unavailable.
func ApplyRDSLegacyCosts(records []Record) {
	for i := range records {
		if !isRDSKind(records[i].Kind) {
			continue
		}
		records[i].EstimatedMonthlyCostUSD = EstimateMonthlyCost(records[i].Kind, records[i].SizeGiB, "", records[i].Region)
		records[i].CostBasis = CostBasisVolumeSizeEstimate
	}
}

func isRDSKind(kind Kind) bool {
	return kind == KindRDSSnapshot || kind == KindRDSClusterSnapshot
}
