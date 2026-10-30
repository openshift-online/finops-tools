package execsummary

import "testing"

func TestComputeMCSummary_AggregatesCostAndClusterCounts(t *testing.T) {
	clusters := []ClusterMCInput{
		{ClusterID: "c1", MCName: "mc-1", MCRegion: "us-east-1", MCAccountID: "111111111111", SCName: "sc-1", Classification: "external"},
		{ClusterID: "c2", MCName: "mc-1", MCRegion: "us-east-1", MCAccountID: "111111111111", SCName: "sc-1", Classification: "internal"},
	}
	costs := []CostRecord{
		{Month: "2026-05", AccountID: "111111111111", Cost: 200},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 100},
		{ClusterID: "c2", Month: "2026-05", VCPUHours: 50},
	}

	got := ComputeMCSummary(clusters, vcpu, costs)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %+v", len(got), got)
	}
	r := got[0]
	if r.MCName != "mc-1" || r.MCRegion != "us-east-1" || r.SCName != "sc-1" {
		t.Errorf("identity mismatch: %+v", r)
	}
	if r.MCCost != 200 {
		t.Errorf("MCCost = %v, want 200", r.MCCost)
	}
	if r.CustomerCount != 2 {
		t.Errorf("CustomerCount = %d, want 2", r.CustomerCount)
	}
	if r.InternalCount != 1 {
		t.Errorf("InternalCount = %d, want 1", r.InternalCount)
	}
	if r.VCPUHours != 150 {
		t.Errorf("VCPUHours = %v, want 150", r.VCPUHours)
	}
}

func TestComputeMCSummary_DistinctClustersDedupeAcrossMultipleUsageRows(t *testing.T) {
	// Same cluster, hypothetically two usage rows in the same month --
	// CustomerCount must still be 1, not 2.
	clusters := []ClusterMCInput{
		{ClusterID: "c1", MCName: "mc-1", Classification: "external"},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 10},
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 20},
	}
	got := ComputeMCSummary(clusters, vcpu, nil)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].CustomerCount != 1 {
		t.Errorf("CustomerCount = %d, want 1", got[0].CustomerCount)
	}
	if got[0].VCPUHours != 30 {
		t.Errorf("VCPUHours = %v, want 30 (summed)", got[0].VCPUHours)
	}
}

func TestComputeMCSummary_SeparatesByMonth(t *testing.T) {
	clusters := []ClusterMCInput{
		{ClusterID: "c1", MCName: "mc-1", MCAccountID: "111111111111", Classification: "external"},
	}
	costs := []CostRecord{
		{Month: "2026-04", AccountID: "111111111111", Cost: 10},
		{Month: "2026-05", AccountID: "111111111111", Cost: 20},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-04", VCPUHours: 5},
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 15},
	}
	got := ComputeMCSummary(clusters, vcpu, costs)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
	}
	if got[0].Month != "2026-04" || got[0].MCCost != 10 || got[0].VCPUHours != 5 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Month != "2026-05" || got[1].MCCost != 20 || got[1].VCPUHours != 15 {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestComputeMCSummary_IgnoresUnknownAccountsAndClusters(t *testing.T) {
	clusters := []ClusterMCInput{
		{ClusterID: "c1", MCName: "mc-1", MCAccountID: "111111111111", Classification: "external"},
	}
	costs := []CostRecord{
		{Month: "2026-05", AccountID: "999999999999", Cost: 500}, // not an MC account -- ignored
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "unknown-cluster", Month: "2026-05", VCPUHours: 999}, // unknown cluster -- ignored
	}
	got := ComputeMCSummary(clusters, vcpu, costs)
	if len(got) != 0 {
		t.Errorf("got = %+v, want empty (no recognized cost/usage)", got)
	}
}

func TestComputeMCSummary_EmptyInputs(t *testing.T) {
	got := ComputeMCSummary(nil, nil, nil)
	if len(got) != 0 {
		t.Errorf("got = %+v, want empty", got)
	}
}
