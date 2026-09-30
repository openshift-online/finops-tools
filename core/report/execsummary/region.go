package execsummary

import (
	"sort"
)

// ClusterRegionInput is the minimal per-worker-cluster data needed for
// region attribution (spec 6.2) and the regional cost/revenue formulas
// (spec 6.1, 6.3, 6.4). Build it from hcphierarchy.HierarchyRow in the
// generator -- this package has no dependency on hcphierarchy so it stays a
// pure, independently-testable formula layer.
type ClusterRegionInput struct {
	// ClusterID matches hcphierarchy.HierarchyRow.CustomerClusterID /
	// telesense.ClusterMonthUsage.ClusterID -- the in-memory join key.
	ClusterID string
	// CustomerRegion, MCRegion, and SCRegion feed the section 6.2 fallback
	// chain (Region()).
	CustomerRegion string
	MCRegion       string
	SCRegion       string
	// MCAccountID and SCAccountID are the AWS account IDs whose Cost
	// Explorer spend is attributed to this cluster's MC/SC region as
	// "direct infrastructure cost" (spec 6.3 part 1).
	MCAccountID string
	SCAccountID string
	// Classification is "internal" or "external" (OCM_MAPPING.CLASSIFICATION).
	Classification string
}

// Region returns the attributed region for a cluster using the spec 6.2
// fallback chain: prefer the worker's own region; if blank, fall back to
// its Management Cluster's region; if still blank, fall back to its
// Service Cluster's region.
func (c ClusterRegionInput) Region() string {
	if c.CustomerRegion != "" {
		return c.CustomerRegion
	}
	if c.MCRegion != "" {
		return c.MCRegion
	}
	return c.SCRegion
}

// ClusterMonthVCPU pairs a cluster (by ClusterID, matching
// ClusterRegionInput.ClusterID) with its vCPU-hours for one month -- the
// in-memory join target between core/hcphierarchy and core/telesense
// (never a SQL join; spec sections 2 and 11.1).
type ClusterMonthVCPU struct {
	ClusterID string
	Month     string // YYYY-MM
	VCPUHours float64
}

// Revenue computes spec 6.1's per-cluster-month revenue: vcpuHours *
// ExternalVCPURatePerHour for external clusters, 0 for internal ones.
// Internal clusters' vCPU-hours still count toward capacity KPIs elsewhere,
// but never generate revenue.
func Revenue(vcpuHours float64, classification string) float64 {
	if !IsExternal(classification) {
		return 0
	}
	return vcpuHours * ExternalVCPURatePerHour
}

// RegionMonthMetrics holds spec 6.3/6.4 per-(region, month) output: the
// two-part regional cost (direct MC/SC infra cost plus proportionally
// allocated shared-network cost) and net revenue.
type RegionMonthMetrics struct {
	Region string
	Month  string
	// InfraCost is direct MC + SC account cost attributed to this region
	// (spec 6.3 part 1).
	InfraCost float64
	// NetworkCost is the shared-infrastructure-account cost allocated to
	// this region proportionally by its share of that month's total
	// vCPU-hours (spec 6.3 part 2).
	NetworkCost float64
	// TotalCost is InfraCost + NetworkCost.
	TotalCost float64
	// Revenue is the sum of per-cluster revenue (spec 6.1) for clusters
	// attributed to this region in this month.
	Revenue float64
	// NetRevenue is Revenue - TotalCost (spec 6.4).
	NetRevenue float64
	// VCPUHours is this region's total vCPU-hours in this month, exposed so
	// callers (e.g. the HCP Cost Efficiency tab) don't need to recompute it.
	VCPUHours float64
}

// ComputeRegionMonthMetrics implements spec sections 6.1, 6.3, and 6.4:
// two-part regional cost and net revenue for every (region, month)
// combination with any activity.
//
// infraAccountIDs is the caller-supplied list of shared-infrastructure AWS
// account IDs whose cost is allocated proportionally by vCPU-hour share
// rather than attributed directly to one region (spec 6's
// DEFAULT_INFRA_ACCOUNTS). This is deliberately a parameter, not a
// hardcoded default in this package -- spec section 4.2 requires real AWS
// account IDs to be supplied via configuration, never committed as literal
// values in source.
//
// months restricts the output to this set of YYYY-MM keys; pass nil/empty
// to include every month present in costs or vcpu.
func ComputeRegionMonthMetrics(
	clusters []ClusterRegionInput,
	vcpu []ClusterMonthVCPU,
	costs []CostRecord,
	infraAccountIDs []string,
	months []string,
) []RegionMonthMetrics {
	clusterByID := make(map[string]ClusterRegionInput, len(clusters))
	mcAccountRegion := make(map[string]string)
	scAccountRegion := make(map[string]string)
	for _, c := range clusters {
		clusterByID[c.ClusterID] = c
		if c.MCAccountID != "" && c.MCRegion != "" {
			mcAccountRegion[c.MCAccountID] = c.MCRegion
		}
		if c.SCAccountID != "" && c.SCRegion != "" {
			scAccountRegion[c.SCAccountID] = c.SCRegion
		}
	}
	infraAccounts := make(map[string]bool, len(infraAccountIDs))
	for _, id := range infraAccountIDs {
		infraAccounts[id] = true
	}

	monthFilter := map[string]bool(nil)
	if len(months) > 0 {
		monthFilter = make(map[string]bool, len(months))
		for _, m := range months {
			monthFilter[m] = true
		}
	}
	monthAllowed := func(m string) bool {
		return monthFilter == nil || monthFilter[m]
	}

	type accum struct {
		infraCost float64
		vcpuHours float64
		revenue   float64
	}
	byMonthRegion := make(map[string]map[string]*accum)
	totalInfraAccountCostByMonth := make(map[string]float64)
	totalVCPUHoursByMonth := make(map[string]float64)

	get := func(month, region string) *accum {
		regions, ok := byMonthRegion[month]
		if !ok {
			regions = make(map[string]*accum)
			byMonthRegion[month] = regions
		}
		a, ok := regions[region]
		if !ok {
			a = &accum{}
			regions[region] = a
		}
		return a
	}

	// Part 1 (direct infra cost) + shared-infra-account totals, from AWS cost rows.
	for _, cr := range costs {
		if !monthAllowed(cr.Month) {
			continue
		}
		if infraAccounts[cr.AccountID] {
			totalInfraAccountCostByMonth[cr.Month] += cr.Cost
			continue
		}
		if region, ok := mcAccountRegion[cr.AccountID]; ok {
			get(cr.Month, region).infraCost += cr.Cost
		}
		if region, ok := scAccountRegion[cr.AccountID]; ok {
			get(cr.Month, region).infraCost += cr.Cost
		}
	}

	// vCPU-hours and revenue, from the in-memory hierarchy/telesense join.
	for _, v := range vcpu {
		if !monthAllowed(v.Month) {
			continue
		}
		totalVCPUHoursByMonth[v.Month] += v.VCPUHours
		c, ok := clusterByID[v.ClusterID]
		if !ok {
			continue // unknown cluster: counts toward the total, not toward any region
		}
		a := get(v.Month, c.Region())
		a.vcpuHours += v.VCPUHours
		a.revenue += Revenue(v.VCPUHours, c.Classification)
	}

	var out []RegionMonthMetrics
	for month, regions := range byMonthRegion {
		totalInfra := totalInfraAccountCostByMonth[month]
		totalVCPU := totalVCPUHoursByMonth[month]
		for region, a := range regions {
			var networkCost float64
			if totalVCPU != 0 {
				networkCost = totalInfra * a.vcpuHours / totalVCPU
			}
			totalCost := a.infraCost + networkCost
			if a.vcpuHours == 0 && totalCost == 0 {
				continue // spec 6.4: exclude cells with no activity at all
			}
			out = append(out, RegionMonthMetrics{
				Region:      region,
				Month:       month,
				InfraCost:   a.infraCost,
				NetworkCost: networkCost,
				TotalCost:   totalCost,
				Revenue:     a.revenue,
				NetRevenue:  a.revenue - totalCost,
				VCPUHours:   a.vcpuHours,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Month != out[j].Month {
			return out[i].Month < out[j].Month
		}
		return out[i].Region < out[j].Region
	})
	return out
}
