package execsummary

import "testing"

func TestCategoryOrderByTotalCost_SortsByTotalDescending(t *testing.T) {
	records := []CategoryMonthRecord{
		{ServiceName: "Compute", Month: "2026-04", Cost: 100},
		{ServiceName: "Compute", Month: "2026-05", Cost: 150},
		{ServiceName: "Storage", Month: "2026-04", Cost: 400},
		{ServiceName: "Storage", Month: "2026-05", Cost: 50},
		{ServiceName: "Network", Month: "2026-04", Cost: 10},
	}
	got := CategoryOrderByTotalCost(records)
	want := []string{"Storage", "Compute", "Network"} // 450, 250, 10
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestCategoryOrderByTotalCost_TiesBreakAlphabetically(t *testing.T) {
	records := []CategoryMonthRecord{
		{ServiceName: "Zebra", Month: "2026-04", Cost: 100},
		{ServiceName: "Alpha", Month: "2026-04", Cost: 100},
	}
	got := CategoryOrderByTotalCost(records)
	if len(got) != 2 || got[0] != "Alpha" || got[1] != "Zebra" {
		t.Errorf("got = %v, want [Alpha Zebra]", got)
	}
}

func TestCategoryOrderByTotalCost_Empty(t *testing.T) {
	got := CategoryOrderByTotalCost(nil)
	if len(got) != 0 {
		t.Errorf("got = %v, want empty", got)
	}
}

func TestBuildClusterDetailRecords_FiltersToMonthAndJoinsRevenue(t *testing.T) {
	clusters := []ClusterDetailInput{
		{ClusterID: "c1", MCName: "mc-a", Region: "us-east-1", Classification: "external", Organization: "org-a", BillingModel: "standard"},
		{ClusterID: "c2", MCName: "mc-a", Region: "us-east-1", Classification: "internal", Organization: "org-b", BillingModel: "standard"},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 1000},
		{ClusterID: "c2", Month: "2026-05", VCPUHours: 2000},
		{ClusterID: "c1", Month: "2026-04", VCPUHours: 500}, // different month, excluded
	}
	got := BuildClusterDetailRecords(clusters, vcpu, "2026-05", 100)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
	}
	// Sorted by VCPUHours descending: c2 (2000) before c1 (1000).
	if got[0].ClusterID != "c2" || got[0].VCPUHours != 2000 {
		t.Errorf("got[0] = %+v, want c2/2000", got[0])
	}
	if got[0].Revenue != 0 {
		t.Errorf("got[0].Revenue = %v, want 0 (internal)", got[0].Revenue)
	}
	if got[1].ClusterID != "c1" || got[1].VCPUHours != 1000 {
		t.Errorf("got[1] = %+v, want c1/1000", got[1])
	}
	wantRevenue := Revenue(1000, "external")
	if got[1].Revenue != wantRevenue {
		t.Errorf("got[1].Revenue = %v, want %v", got[1].Revenue, wantRevenue)
	}
}

func TestBuildClusterDetailRecords_TruncatesToLimit(t *testing.T) {
	clusters := []ClusterDetailInput{
		{ClusterID: "c1", Classification: "external"},
		{ClusterID: "c2", Classification: "external"},
		{ClusterID: "c3", Classification: "external"},
	}
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "c1", Month: "2026-05", VCPUHours: 100},
		{ClusterID: "c2", Month: "2026-05", VCPUHours: 200},
		{ClusterID: "c3", Month: "2026-05", VCPUHours: 300},
	}
	got := BuildClusterDetailRecords(clusters, vcpu, "2026-05", 2)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].ClusterID != "c3" || got[1].ClusterID != "c2" {
		t.Errorf("got = %+v, want [c3 c2] (top 2 by vCPU hours)", got)
	}
}

func TestBuildClusterDetailRecords_SkipsUnknownClusters(t *testing.T) {
	vcpu := []ClusterMonthVCPU{
		{ClusterID: "unknown", Month: "2026-05", VCPUHours: 100},
	}
	got := BuildClusterDetailRecords(nil, vcpu, "2026-05", 100)
	if len(got) != 0 {
		t.Errorf("got = %+v, want empty (unknown cluster skipped)", got)
	}
}
