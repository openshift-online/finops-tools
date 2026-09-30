package execsummary

import (
	"testing"
)

func TestClusterRegionInput_Region_FallbackChain(t *testing.T) {
	cases := []struct {
		name string
		c    ClusterRegionInput
		want string
	}{
		{"prefers customer region", ClusterRegionInput{CustomerRegion: "us-east-1", MCRegion: "us-west-2", SCRegion: "eu-west-1"}, "us-east-1"},
		{"falls back to mc region", ClusterRegionInput{MCRegion: "us-west-2", SCRegion: "eu-west-1"}, "us-west-2"},
		{"falls back to sc region", ClusterRegionInput{SCRegion: "eu-west-1"}, "eu-west-1"},
		{"all blank", ClusterRegionInput{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.Region(); got != tc.want {
				t.Errorf("Region() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsExternal(t *testing.T) {
	if !IsExternal("external") {
		t.Error("IsExternal(\"external\") = false, want true")
	}
	if IsExternal("internal") {
		t.Error("IsExternal(\"internal\") = true, want false")
	}
	if IsExternal("") {
		t.Error("IsExternal(\"\") = true, want false")
	}
}

func TestRevenue(t *testing.T) {
	cases := []struct {
		name           string
		vcpuHours      float64
		classification string
		want           float64
	}{
		{"external earns revenue", 1000, "external", 1000 * ExternalVCPURatePerHour},
		{"internal earns nothing", 1000, "internal", 0},
		{"unknown classification earns nothing", 1000, "", 0},
		{"zero hours earns nothing", 0, "external", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Revenue(tc.vcpuHours, tc.classification); got != tc.want {
				t.Errorf("Revenue(%v, %q) = %v, want %v", tc.vcpuHours, tc.classification, got, tc.want)
			}
		})
	}
}

func TestComputeRegionMonthMetrics_DirectInfraCostByRegion(t *testing.T) {
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", CustomerRegion: "us-east-1", MCAccountID: "111111111111", MCRegion: "us-east-1", Classification: "external"},
		{ClusterID: "c2", CustomerRegion: "us-west-2", MCAccountID: "222222222222", MCRegion: "us-west-2", Classification: "external"},
	}
	costs := []CostRecord{
		{Month: "2026-05", AccountID: "111111111111", Cost: 100},
		{Month: "2026-05", AccountID: "222222222222", Cost: 50},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 100},
		{ClusterID: "c2", Month: "2026-05", VCPUHours: 100},
	}

	got := ComputeRegionMonthMetrics(clusters, vcpu, costs, nil, nil)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
	}

	byRegion := map[string]RegionMonthMetrics{}
	for _, m := range got {
		byRegion[m.Region] = m
	}

	east := byRegion["us-east-1"]
	if east.InfraCost != 100 {
		t.Errorf("us-east-1 InfraCost = %v, want 100", east.InfraCost)
	}
	if east.NetworkCost != 0 {
		t.Errorf("us-east-1 NetworkCost = %v, want 0 (no infra accounts configured)", east.NetworkCost)
	}
	if east.TotalCost != 100 {
		t.Errorf("us-east-1 TotalCost = %v, want 100", east.TotalCost)
	}
	wantRevenue := 100 * ExternalVCPURatePerHour
	if east.Revenue != wantRevenue {
		t.Errorf("us-east-1 Revenue = %v, want %v", east.Revenue, wantRevenue)
	}
	if east.NetRevenue != wantRevenue-100 {
		t.Errorf("us-east-1 NetRevenue = %v, want %v", east.NetRevenue, wantRevenue-100)
	}
}

func TestComputeRegionMonthMetrics_NetworkCostProportionalToVCPUShare(t *testing.T) {
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", CustomerRegion: "us-east-1", Classification: "external"},
		{ClusterID: "c2", CustomerRegion: "us-west-2", Classification: "external"},
	}
	costs := []CostRecord{
		// Shared infra account: $400 total for the month.
		{Month: "2026-05", AccountID: "333333333333", Cost: 400},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 300}, // 75% of total
		{ClusterID: "c2", Month: "2026-05", VCPUHours: 100}, // 25% of total
	}

	got := ComputeRegionMonthMetrics(clusters, vcpu, costs, []string{"333333333333"}, nil)
	byRegion := map[string]RegionMonthMetrics{}
	for _, m := range got {
		byRegion[m.Region] = m
	}

	east := byRegion["us-east-1"]
	if east.InfraCost != 0 {
		t.Errorf("us-east-1 InfraCost = %v, want 0", east.InfraCost)
	}
	if east.NetworkCost != 300 {
		t.Errorf("us-east-1 NetworkCost = %v, want 300 (75%% of 400)", east.NetworkCost)
	}

	west := byRegion["us-west-2"]
	if west.NetworkCost != 100 {
		t.Errorf("us-west-2 NetworkCost = %v, want 100 (25%% of 400)", west.NetworkCost)
	}
}

func TestComputeRegionMonthMetrics_ZeroTotalVCPUHoursAvoidsDivideByZero(t *testing.T) {
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", MCAccountID: "111111111111", MCRegion: "us-east-1", Classification: "external"},
	}
	costs := []CostRecord{
		{Month: "2026-05", AccountID: "111111111111", Cost: 50},
		{Month: "2026-05", AccountID: "333333333333", Cost: 400}, // infra account, but no vCPU hours recorded anywhere
	}

	got := ComputeRegionMonthMetrics(clusters, nil, costs, []string{"333333333333"}, nil)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %+v", len(got), got)
	}
	if got[0].NetworkCost != 0 {
		t.Errorf("NetworkCost = %v, want 0 when total vCPU hours is 0", got[0].NetworkCost)
	}
	if got[0].TotalCost != 50 {
		t.Errorf("TotalCost = %v, want 50", got[0].TotalCost)
	}
}

func TestComputeRegionMonthMetrics_ExcludesEmptyActivityCells(t *testing.T) {
	// A region with zero cost and zero vCPU-hours should never appear, even
	// if it's referenced in an mc/sc account map with no matching cost row.
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", CustomerRegion: "us-east-1", MCAccountID: "111111111111", MCRegion: "us-east-1", Classification: "external"},
	}
	// No cost rows at all, no vcpu rows at all -- nothing should be emitted.
	got := ComputeRegionMonthMetrics(clusters, nil, nil, nil, nil)
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0 for no activity at all: %+v", len(got), got)
	}
}

func TestComputeRegionMonthMetrics_IncludesCellWithVCPUButNoCost(t *testing.T) {
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", CustomerRegion: "us-east-1", Classification: "internal"},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 50},
	}
	got := ComputeRegionMonthMetrics(clusters, vcpu, nil, nil, nil)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (vcpu_hours > 0 keeps the cell)", len(got))
	}
	if got[0].Revenue != 0 {
		t.Errorf("Revenue = %v, want 0 for internal cluster", got[0].Revenue)
	}
	if got[0].TotalCost != 0 {
		t.Errorf("TotalCost = %v, want 0", got[0].TotalCost)
	}
}

func TestComputeRegionMonthMetrics_IncludesCellWithCostButNoVCPU(t *testing.T) {
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", MCAccountID: "111111111111", MCRegion: "us-east-1", Classification: "external"},
	}
	costs := []CostRecord{
		{Month: "2026-05", AccountID: "111111111111", Cost: 25},
	}
	got := ComputeRegionMonthMetrics(clusters, nil, costs, nil, nil)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (total_cost > 0 keeps the cell)", len(got))
	}
	if got[0].VCPUHours != 0 {
		t.Errorf("VCPUHours = %v, want 0", got[0].VCPUHours)
	}
	if got[0].TotalCost != 25 {
		t.Errorf("TotalCost = %v, want 25", got[0].TotalCost)
	}
}

func TestComputeRegionMonthMetrics_FiltersByMonth(t *testing.T) {
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", MCAccountID: "111111111111", MCRegion: "us-east-1", Classification: "external"},
	}
	costs := []CostRecord{
		{Month: "2026-04", AccountID: "111111111111", Cost: 10},
		{Month: "2026-05", AccountID: "111111111111", Cost: 20},
	}
	got := ComputeRegionMonthMetrics(clusters, nil, costs, nil, []string{"2026-05"})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (only 2026-05 allowed): %+v", len(got), got)
	}
	if got[0].Month != "2026-05" || got[0].TotalCost != 20 {
		t.Errorf("got[0] = %+v, want month 2026-05 cost 20", got[0])
	}
}

func TestComputeRegionMonthMetrics_UnknownClusterStillCountsTowardMonthTotal(t *testing.T) {
	// c1 is known (region us-east-1); c2 is not in the clusters list at all.
	// c2's vCPU-hours should still dilute the network-cost denominator even
	// though they can't be attributed to any specific region.
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", CustomerRegion: "us-east-1", Classification: "external"},
	}
	costs := []CostRecord{
		{Month: "2026-05", AccountID: "333333333333", Cost: 100},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 50},
		{ClusterID: "c2-unknown", Month: "2026-05", VCPUHours: 50},
	}

	got := ComputeRegionMonthMetrics(clusters, vcpu, costs, []string{"333333333333"}, nil)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %+v", len(got), got)
	}
	// Total vCPU hours = 100 (50 known + 50 unknown); us-east-1 gets 50/100 = 50% of the $100 network cost.
	if got[0].NetworkCost != 50 {
		t.Errorf("NetworkCost = %v, want 50 (unknown cluster dilutes the denominator)", got[0].NetworkCost)
	}
}

func TestComputeRegionMonthMetrics_SortedByMonthThenRegion(t *testing.T) {
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", MCAccountID: "111111111111", MCRegion: "us-west-2", Classification: "external"},
		{ClusterID: "c2", MCAccountID: "222222222222", MCRegion: "us-east-1", Classification: "external"},
	}
	costs := []CostRecord{
		{Month: "2026-05", AccountID: "111111111111", Cost: 10},
		{Month: "2026-05", AccountID: "222222222222", Cost: 10},
		{Month: "2026-04", AccountID: "111111111111", Cost: 10},
	}
	got := ComputeRegionMonthMetrics(clusters, nil, costs, nil, nil)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0].Month != "2026-04" {
		t.Errorf("got[0].Month = %q, want 2026-04 first", got[0].Month)
	}
	if got[1].Month != "2026-05" || got[1].Region != "us-east-1" {
		t.Errorf("got[1] = %+v, want month 2026-05 region us-east-1 first", got[1])
	}
	if got[2].Month != "2026-05" || got[2].Region != "us-west-2" {
		t.Errorf("got[2] = %+v, want month 2026-05 region us-west-2 second", got[2])
	}
}

func TestComputeRegionMonthMetrics_MCAndSCCostsCombineInSameRegion(t *testing.T) {
	clusters := []ClusterRegionInput{
		{
			ClusterID:      "c1",
			CustomerRegion: "us-east-1",
			MCAccountID:    "111111111111",
			MCRegion:       "us-east-1",
			SCAccountID:    "222222222222",
			SCRegion:       "us-east-1",
			Classification: "external",
		},
	}
	costs := []CostRecord{
		{Month: "2026-05", AccountID: "111111111111", Cost: 30}, // MC cost
		{Month: "2026-05", AccountID: "222222222222", Cost: 20}, // SC cost
	}
	got := ComputeRegionMonthMetrics(clusters, nil, costs, nil, nil)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %+v", len(got), got)
	}
	if got[0].InfraCost != 50 {
		t.Errorf("InfraCost = %v, want 50 (30 MC + 20 SC)", got[0].InfraCost)
	}
}
