package execsummary

import "sort"

// VCPUEfficiencyRecord is one row of the spec 6.6 vCPU efficiency table,
// one row per (Classification, Month).
type VCPUEfficiencyRecord struct {
	Classification string
	Month          string
	VCPUHours      float64
	// AllocatedCost is total_cost(month) weighted by this classification's
	// share of that month's total vCPU-hours.
	AllocatedCost float64
	// CostPerVCPU is AllocatedCost / VCPUHours (0 if VCPUHours is 0).
	CostPerVCPU float64
	// RevenuePerVCPU is ExternalVCPURatePerHour for external classifications,
	// else 0.
	RevenuePerVCPU float64
	// NetMarginPerVCPU is RevenuePerVCPU - CostPerVCPU for external
	// classifications, else 0.
	NetMarginPerVCPU float64
}

// ComputeVCPUEfficiency implements spec 6.6: cost, revenue, and margin per
// vCPU-hour, allocated between classifications (e.g. "internal"/"external")
// using each classification's share of that month's total vCPU-hours as a
// proxy weight. regionMetrics supplies total_cost(month) -- the sum of
// RegionMonthMetrics.TotalCost across all regions for that month, i.e. the
// output of ComputeRegionMonthMetrics.
func ComputeVCPUEfficiency(
	clusters []ClusterRegionInput,
	vcpu []ClusterMonthVCPU,
	regionMetrics []RegionMonthMetrics,
) []VCPUEfficiencyRecord {
	clusterByID := make(map[string]ClusterRegionInput, len(clusters))
	for _, c := range clusters {
		clusterByID[c.ClusterID] = c
	}

	totalCostByMonth := make(map[string]float64)
	for _, m := range regionMetrics {
		totalCostByMonth[m.Month] += m.TotalCost
	}

	type key struct{ classification, month string }
	vcpuByKey := make(map[key]float64)
	totalVCPUByMonth := make(map[string]float64)

	for _, v := range vcpu {
		// Every cluster's vCPU-hours count toward the month's total
		// (matching ComputeRegionMonthMetrics' denominator semantics), even
		// when the cluster can't be attributed to a classification.
		totalVCPUByMonth[v.Month] += v.VCPUHours
		c, ok := clusterByID[v.ClusterID]
		if !ok {
			continue
		}
		vcpuByKey[key{c.Classification, v.Month}] += v.VCPUHours
	}

	out := make([]VCPUEfficiencyRecord, 0, len(vcpuByKey))
	for k, hours := range vcpuByKey {
		totalVCPU := totalVCPUByMonth[k.month]
		totalCost := totalCostByMonth[k.month]

		var fraction float64
		if totalVCPU != 0 {
			fraction = hours / totalVCPU
		}
		allocated := totalCost * fraction

		var costPerVCPU float64
		if hours != 0 {
			costPerVCPU = allocated / hours
		}

		var revenuePerVCPU, netMargin float64
		if IsExternal(k.classification) {
			revenuePerVCPU = ExternalVCPURatePerHour
			netMargin = revenuePerVCPU - costPerVCPU
		}

		out = append(out, VCPUEfficiencyRecord{
			Classification:   k.classification,
			Month:            k.month,
			VCPUHours:        hours,
			AllocatedCost:    allocated,
			CostPerVCPU:      costPerVCPU,
			RevenuePerVCPU:   revenuePerVCPU,
			NetMarginPerVCPU: netMargin,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Month != out[j].Month {
			return out[i].Month < out[j].Month
		}
		return out[i].Classification < out[j].Classification
	})
	return out
}

// CalculateUnitCost returns the $/1,000-vCPU-hour unit cost implied by
// totalCost and revenue (spec 6.6's calculate_unit_cost). Returns 0 if
// revenue is 0.
func CalculateUnitCost(totalCost, revenue float64) float64 {
	if revenue == 0 {
		return 0
	}
	return (totalCost / revenue) * ExternalVCPURatePer1KHour
}

// CalculateMargin returns the profit margin percentage implied by revenue
// and cost (spec 6.6's calculate_margin). Returns 0 if revenue is 0.
func CalculateMargin(revenue, cost float64) float64 {
	if revenue == 0 {
		return 0
	}
	return ((revenue - cost) / revenue) * 100
}
