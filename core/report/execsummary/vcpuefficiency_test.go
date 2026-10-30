package execsummary

import "testing"

func TestComputeVCPUEfficiency_AllocatesCostByVCPUShare(t *testing.T) {
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", Classification: "external"},
		{ClusterID: "c2", Classification: "internal"},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 300}, // 75%
		{ClusterID: "c2", Month: "2026-05", VCPUHours: 100}, // 25%
	}
	regionMetrics := []RegionMonthMetrics{
		{Region: "us-east-1", Month: "2026-05", TotalCost: 400},
	}

	got := ComputeVCPUEfficiency(clusters, vcpu, regionMetrics)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
	}

	byClass := map[string]VCPUEfficiencyRecord{}
	for _, r := range got {
		byClass[r.Classification] = r
	}

	ext := byClass["external"]
	if ext.AllocatedCost != 300 {
		t.Errorf("external AllocatedCost = %v, want 300 (75%% of 400)", ext.AllocatedCost)
	}
	wantCostPerVCPU := 1.0 // allocated $300 over 300 vCPU-hours
	if ext.CostPerVCPU != wantCostPerVCPU {
		t.Errorf("external CostPerVCPU = %v, want %v", ext.CostPerVCPU, wantCostPerVCPU)
	}
	if ext.RevenuePerVCPU != ExternalVCPURatePerHour {
		t.Errorf("external RevenuePerVCPU = %v, want %v", ext.RevenuePerVCPU, ExternalVCPURatePerHour)
	}
	wantMargin := ExternalVCPURatePerHour - wantCostPerVCPU
	if ext.NetMarginPerVCPU != wantMargin {
		t.Errorf("external NetMarginPerVCPU = %v, want %v", ext.NetMarginPerVCPU, wantMargin)
	}

	internal := byClass["internal"]
	if internal.AllocatedCost != 100 {
		t.Errorf("internal AllocatedCost = %v, want 100 (25%% of 400)", internal.AllocatedCost)
	}
	if internal.RevenuePerVCPU != 0 || internal.NetMarginPerVCPU != 0 {
		t.Errorf("internal revenue/margin should be 0, got %+v", internal)
	}
}

func TestComputeVCPUEfficiency_ZeroVCPUHoursAvoidsDivideByZero(t *testing.T) {
	clusters := []ClusterRegionInput{{ClusterID: "c1", Classification: "external"}}
	// No vcpu rows at all -> no output, no panic.
	got := ComputeVCPUEfficiency(clusters, nil, nil)
	if len(got) != 0 {
		t.Errorf("got = %+v, want empty", got)
	}
}

func TestComputeVCPUEfficiency_UnknownClusterDilutesDenominatorOnly(t *testing.T) {
	clusters := []ClusterRegionInput{
		{ClusterID: "c1", Classification: "external"},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 50},
		{ClusterID: "unknown", Month: "2026-05", VCPUHours: 50},
	}
	regionMetrics := []RegionMonthMetrics{
		{Region: "us-east-1", Month: "2026-05", TotalCost: 100},
	}
	got := ComputeVCPUEfficiency(clusters, vcpu, regionMetrics)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (no row for unknown cluster): %+v", len(got), got)
	}
	// External's share of total (50/100 = 50%) allocated cost = 50.
	if got[0].AllocatedCost != 50 {
		t.Errorf("AllocatedCost = %v, want 50 (dilution from unknown cluster)", got[0].AllocatedCost)
	}
}

func TestComputeVCPUEfficiency_SumsTotalCostAcrossRegionsPerMonth(t *testing.T) {
	clusters := []ClusterRegionInput{{ClusterID: "c1", Classification: "external"}}
	vcpu := []ClusterMonthVCPU{{ClusterID: "c1", Month: "2026-05", VCPUHours: 100}}
	regionMetrics := []RegionMonthMetrics{
		{Region: "us-east-1", Month: "2026-05", TotalCost: 100},
		{Region: "us-west-2", Month: "2026-05", TotalCost: 50},
	}
	got := ComputeVCPUEfficiency(clusters, vcpu, regionMetrics)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].AllocatedCost != 150 {
		t.Errorf("AllocatedCost = %v, want 150 (full share of summed regional total)", got[0].AllocatedCost)
	}
}

func TestCalculateUnitCost(t *testing.T) {
	got := CalculateUnitCost(100, 50)
	want := (100.0 / 50.0) * ExternalVCPURatePer1KHour
	if got != want {
		t.Errorf("CalculateUnitCost(100,50) = %v, want %v", got, want)
	}
	if got := CalculateUnitCost(100, 0); got != 0 {
		t.Errorf("CalculateUnitCost(100,0) = %v, want 0", got)
	}
}

func TestCalculateMargin(t *testing.T) {
	got := CalculateMargin(200, 50)
	want := ((200.0 - 50.0) / 200.0) * 100
	if got != want {
		t.Errorf("CalculateMargin(200,50) = %v, want %v", got, want)
	}
	if got := CalculateMargin(0, 50); got != 0 {
		t.Errorf("CalculateMargin(0,50) = %v, want 0", got)
	}
}
