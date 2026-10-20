package execsummary

import "sort"

// ClusterMCInput is the minimal per-worker-cluster data needed for the
// MC-level summary (spec 6.5). Build it from hcphierarchy.HierarchyRow --
// this package has no dependency on hcphierarchy so it stays a pure,
// independently-testable formula layer.
type ClusterMCInput struct {
	// ClusterID matches ClusterMonthVCPU.ClusterID -- the in-memory join key.
	ClusterID string
	MCName    string
	MCRegion  string
	// MCAccountID is the AWS account ID whose Cost Explorer spend is this
	// cluster's Management Cluster's cost.
	MCAccountID string
	SCName      string
	// Classification is "internal" or "external" (OCM_MAPPING.CLASSIFICATION).
	Classification string
}

// MCSummaryRecord is one row of the spec 6.5 MC-level summary table, keyed
// by (MCName, MCRegion, SCName, Month).
type MCSummaryRecord struct {
	MCName   string
	MCRegion string
	SCName   string
	Month    string
	// MCCost is this MC account's AWS cost in Month.
	MCCost float64
	// CustomerCount is the number of distinct clusters under this MC with
	// vCPU-hour activity in Month, regardless of classification.
	CustomerCount int
	// InternalCount is the subset of CustomerCount with a non-external
	// classification.
	InternalCount int
	// VCPUHours is the sum of vCPU-hours for clusters under this MC in Month.
	VCPUHours float64
}

// ComputeMCSummary implements spec 6.5: for each (MCName, month) with any
// cost or vCPU-hour activity, compute the MC account's AWS cost, distinct
// active cluster counts (total and internal-only), and total vCPU-hours.
// MCRegion and SCName are constant per MCName and are looked up from the
// first matching row in clusters.
func ComputeMCSummary(clusters []ClusterMCInput, vcpu []ClusterMonthVCPU, costs []CostRecord) []MCSummaryRecord {
	clusterByID := make(map[string]ClusterMCInput, len(clusters))
	type mcMeta struct {
		region, scName, accountID string
	}
	mcInfo := make(map[string]mcMeta)
	for _, c := range clusters {
		clusterByID[c.ClusterID] = c
		if _, ok := mcInfo[c.MCName]; !ok {
			mcInfo[c.MCName] = mcMeta{region: c.MCRegion, scName: c.SCName, accountID: c.MCAccountID}
		}
	}

	mcNameByAccount := make(map[string]string, len(mcInfo))
	for mcName, info := range mcInfo {
		if info.accountID != "" {
			mcNameByAccount[info.accountID] = mcName
		}
	}

	type key struct{ mcName, month string }
	type accum struct {
		mcCost       float64
		vcpuHours    float64
		customerSeen map[string]bool
		internalSeen map[string]bool
	}
	groups := make(map[key]*accum)
	get := func(mcName, month string) *accum {
		k := key{mcName, month}
		a, ok := groups[k]
		if !ok {
			a = &accum{customerSeen: map[string]bool{}, internalSeen: map[string]bool{}}
			groups[k] = a
		}
		return a
	}

	for _, cr := range costs {
		mcName, ok := mcNameByAccount[cr.AccountID]
		if !ok {
			continue
		}
		get(mcName, cr.Month).mcCost += cr.Cost
	}

	for _, v := range vcpu {
		c, ok := clusterByID[v.ClusterID]
		if !ok || c.MCName == "" {
			continue
		}
		a := get(c.MCName, v.Month)
		a.vcpuHours += v.VCPUHours
		a.customerSeen[c.ClusterID] = true
		if !IsExternal(c.Classification) {
			a.internalSeen[c.ClusterID] = true
		}
	}

	out := make([]MCSummaryRecord, 0, len(groups))
	for k, a := range groups {
		info := mcInfo[k.mcName]
		out = append(out, MCSummaryRecord{
			MCName:        k.mcName,
			MCRegion:      info.region,
			SCName:        info.scName,
			Month:         k.month,
			MCCost:        a.mcCost,
			CustomerCount: len(a.customerSeen),
			InternalCount: len(a.internalSeen),
			VCPUHours:     a.vcpuHours,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Month != out[j].Month {
			return out[i].Month < out[j].Month
		}
		return out[i].MCName < out[j].MCName
	})
	return out
}
