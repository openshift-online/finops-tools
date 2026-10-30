package execsummary

// MoMPct implements spec 6.10's month-over-month percentage formula.
// Returns 0 if totalPrev <= 0.
func MoMPct(totalLast, totalPrev float64) float64 {
	if totalPrev <= 0 {
		return 0
	}
	return (totalLast - totalPrev) / totalPrev * 100
}

// MoMClass returns the spec 6.10 KPI-card color class for a
// month-over-month percentage: "bad" if momPct > 5, "warn" if momPct > 0,
// else "good".
func MoMClass(momPct float64) string {
	switch {
	case momPct > 5:
		return "bad"
	case momPct > 0:
		return "warn"
	default:
		return "good"
	}
}

// SPCoverageClass returns the spec 6.10 KPI-card color class for a Savings
// Plans coverage percentage: "good" if coveragePct >= 70, else "warn".
func SPCoverageClass(coveragePct float64) string {
	if coveragePct >= 70 {
		return "good"
	}
	return "warn"
}

// SPUtilizationClass returns the spec 6.10 KPI-card color class for a
// Savings Plans utilization percentage: "good" if utilizationPct >= 90,
// else "warn".
func SPUtilizationClass(utilizationPct float64) string {
	if utilizationPct >= 90 {
		return "good"
	}
	return "warn"
}

// SavingsRate implements spec 6.10's savings-rate formula. Returns nil if
// totalOnDemandEquiv is 0 ("null if total_on_demand_equiv == 0").
func SavingsRate(totalNetSavings, totalOnDemandEquiv float64) *float64 {
	if totalOnDemandEquiv == 0 {
		return nil
	}
	rate := (totalNetSavings / totalOnDemandEquiv) * 100
	return &rate
}

// SavingsRateClass returns the spec 6.10 KPI-card color class for a
// savings-rate percentage: "warn" if ratePct < 20, else "good".
func SavingsRateClass(ratePct float64) string {
	if ratePct < 20 {
		return "warn"
	}
	return "good"
}

// AnomalyClass returns the spec 6.10 KPI-card color class for an anomaly
// count: "bad" if count > 5, "warn" if count > 0, else "good".
func AnomalyClass(count int) string {
	switch {
	case count > 5:
		return "bad"
	case count > 0:
		return "warn"
	default:
		return "good"
	}
}

// PayerKPIInputs bundles everything ComputePayerKPIs needs to assemble one
// payer's (or the all-payers aggregate's) KPI card (spec 6.10, 6.11).
type PayerKPIInputs struct {
	PayerLabel string

	// CostRecords is this payer's (or the combined) AWS cost rows for the
	// whole window; only Month, AccountID, and Cost are used here.
	CostRecords []CostRecord
	// LastMonth and PrevMonth are the two most recent YYYY-MM keys in the
	// report window.
	LastMonth, PrevMonth string

	// HCPAccountIDs is the set of AWS account IDs considered "HCP" (MC/SC
	// accounts) for the HCPCost subset.
	HCPAccountIDs map[string]bool

	// ClusterCumulativeByMonth is month -> cumulative active cluster count
	// for this payer's environment (e.g. the output of CumulativeCounts).
	ClusterCumulativeByMonth map[string]int

	// SPCoverageAvg and SPUtilizationAvg are already combined across payers
	// per spec 3.3 (see CombinePercentageAcrossPayers / AveragePercentage).
	SPCoverageAvg    *float64
	SPUtilizationAvg *float64
	// TotalNetSavings and TotalOnDemandEquiv are already combined/summed
	// across payers per spec 3.3 (see TotalSavings).
	TotalNetSavings    float64
	TotalOnDemandEquiv float64

	// AnomalyCount is the number of statistical anomalies detected for this
	// payer/window (e.g. len(StatisticalAnomalies(...))).
	AnomalyCount int
}

// ComputePayerKPIs assembles a PayerKPIs card from raw inputs, applying
// every spec 6.10/6.11 formula and color-class threshold.
func ComputePayerKPIs(in PayerKPIInputs) PayerKPIs {
	var lastTotal, prevTotal, hcpCost float64
	for _, r := range in.CostRecords {
		switch r.Month {
		case in.LastMonth:
			lastTotal += r.Cost
			if in.HCPAccountIDs[r.AccountID] {
				hcpCost += r.Cost
			}
		case in.PrevMonth:
			prevTotal += r.Cost
		}
	}

	momPct := MoMPct(lastTotal, prevTotal)
	clusterCount := in.ClusterCumulativeByMonth[in.LastMonth]
	clusterDelta := ClusterMoMDelta(in.ClusterCumulativeByMonth, in.LastMonth, in.PrevMonth)

	var hcpUnitCost *float64
	if clusterCount != 0 {
		v := hcpCost / float64(clusterCount)
		hcpUnitCost = &v
	}

	savingsRate := SavingsRate(in.TotalNetSavings, in.TotalOnDemandEquiv)

	k := PayerKPIs{
		PayerLabel:         in.PayerLabel,
		TotalCostLastMonth: lastTotal,
		TotalCostPrevMonth: prevTotal,
		MoMPct:             momPct,
		MoMClass:           MoMClass(momPct),
		ClusterCount:       clusterCount,
		ClusterDelta:       clusterDelta,
		HCPCost:            hcpCost,
		HCPUnitCost:        hcpUnitCost,
		SPCoverageAvg:      in.SPCoverageAvg,
		SPUtilizationAvg:   in.SPUtilizationAvg,
		TotalNetSavings:    in.TotalNetSavings,
		TotalOnDemandEquiv: in.TotalOnDemandEquiv,
		SavingsRate:        savingsRate,
		AnomalyCount:       in.AnomalyCount,
		AnomalyClass:       AnomalyClass(in.AnomalyCount),
	}
	if in.SPCoverageAvg != nil {
		k.SPCoverageClass = SPCoverageClass(*in.SPCoverageAvg)
	}
	if in.SPUtilizationAvg != nil {
		k.SPUtilizationClass = SPUtilizationClass(*in.SPUtilizationAvg)
	}
	if savingsRate != nil {
		k.SavingsRateClass = SavingsRateClass(*savingsRate)
	}
	return k
}
