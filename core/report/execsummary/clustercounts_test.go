package execsummary

import "testing"

func TestCumulativeCounts_RunningTotalWithinWindow(t *testing.T) {
	newByMonth := map[string]int{
		"2026-01": 3,
		"2026-02": 5,
		"2026-03": 2,
	}
	got := CumulativeCounts(newByMonth, []string{"2026-01", "2026-02", "2026-03"})
	want := map[string]int{"2026-01": 3, "2026-02": 8, "2026-03": 10}
	for m, w := range want {
		if got[m] != w {
			t.Errorf("got[%q] = %d, want %d", m, got[m], w)
		}
	}
}

func TestCumulativeCounts_CarriesForwardOverGapMonths(t *testing.T) {
	// No new entries in Feb -- cumulative should stay flat, not gap.
	newByMonth := map[string]int{
		"2026-01": 10,
		"2026-03": 4,
	}
	got := CumulativeCounts(newByMonth, []string{"2026-01", "2026-02", "2026-03"})
	if got["2026-01"] != 10 {
		t.Errorf("2026-01 = %d, want 10", got["2026-01"])
	}
	if got["2026-02"] != 10 {
		t.Errorf("2026-02 = %d, want 10 (carried forward, not yet including March)", got["2026-02"])
	}
	if got["2026-03"] != 14 {
		t.Errorf("2026-03 = %d, want 14", got["2026-03"])
	}
}

func TestCumulativeCounts_IncludesHistoryBeforeWindowStart(t *testing.T) {
	// Clusters created before the reporting window still count toward the
	// window's first month's cumulative total.
	newByMonth := map[string]int{
		"2025-11": 50, // before the window
		"2025-12": 10, // before the window
		"2026-01": 5,  // in the window
	}
	got := CumulativeCounts(newByMonth, []string{"2026-01", "2026-02"})
	if got["2026-01"] != 65 {
		t.Errorf("2026-01 = %d, want 65 (50+10+5 all-time history)", got["2026-01"])
	}
	if got["2026-02"] != 65 {
		t.Errorf("2026-02 = %d, want 65 (carried forward)", got["2026-02"])
	}
}

func TestCumulativeCounts_EmptyHistory(t *testing.T) {
	got := CumulativeCounts(map[string]int{}, []string{"2026-01", "2026-02"})
	if got["2026-01"] != 0 || got["2026-02"] != 0 {
		t.Errorf("got = %+v, want all zeros", got)
	}
}

func TestCumulativeCounts_EmptyMonths(t *testing.T) {
	got := CumulativeCounts(map[string]int{"2026-01": 5}, nil)
	if len(got) != 0 {
		t.Errorf("got = %+v, want empty map for no requested months", got)
	}
}

func TestClusterMoMDelta_ComputesDifference(t *testing.T) {
	cumulative := map[string]int{"2026-04": 100, "2026-05": 115}
	got := ClusterMoMDelta(cumulative, "2026-05", "2026-04")
	if got == nil || *got != 15 {
		t.Errorf("got = %v, want 15", got)
	}
}

func TestClusterMoMDelta_NilWhenPriorMonthMissing(t *testing.T) {
	cumulative := map[string]int{"2026-05": 115}
	got := ClusterMoMDelta(cumulative, "2026-05", "2026-04")
	if got != nil {
		t.Errorf("got = %v, want nil (no prior-month snapshot)", *got)
	}
}

func TestClusterMoMDelta_NilWhenCurrentMonthMissing(t *testing.T) {
	cumulative := map[string]int{"2026-04": 100}
	got := ClusterMoMDelta(cumulative, "2026-05", "2026-04")
	if got != nil {
		t.Errorf("got = %v, want nil", *got)
	}
}

func TestClusterMoMDelta_NegativeDelta(t *testing.T) {
	cumulative := map[string]int{"2026-04": 100, "2026-05": 90}
	got := ClusterMoMDelta(cumulative, "2026-05", "2026-04")
	if got == nil || *got != -10 {
		t.Errorf("got = %v, want -10", got)
	}
}

func TestSortProductTypes_CanonicalOrderFirst(t *testing.T) {
	got := SortProductTypes([]string{"OSD Trial", "Zebra Product", "OSD", "ROSA Classic", "ROSA HCP"})
	want := []string{"ROSA HCP", "ROSA Classic", "OSD", "OSD Trial", "Zebra Product"}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestSortProductTypes_UnlistedProductsAlphabeticalAfterCanonical(t *testing.T) {
	got := SortProductTypes([]string{"Zebra", "Alpha", "OSD"})
	want := []string{"OSD", "Alpha", "Zebra"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildClusterGrowthPoints_CumulativeAndDeltaPerProduct(t *testing.T) {
	rows := []ClusterGrowthInput{
		{Month: "2025-12", ProductType: "ROSA HCP", NewClusters: 10}, // before window
		{Month: "2026-01", ProductType: "ROSA HCP", NewClusters: 5},
		{Month: "2026-02", ProductType: "ROSA HCP", NewClusters: 3},
		{Month: "2026-01", ProductType: "OSD", NewClusters: 2},
	}
	months := []string{"2026-01", "2026-02"}
	got := BuildClusterGrowthPoints(rows, months)

	if len(got) != 4 { // 2 products x 2 months
		t.Fatalf("len(got) = %d, want 4: %+v", len(got), got)
	}
	// ROSA HCP sorts before OSD per canonical order.
	if got[0].ProductType != "ROSA HCP" || got[0].Month != "2026-01" {
		t.Fatalf("got[0] = %+v, want ROSA HCP/2026-01 first", got[0])
	}
	if got[0].Cumulative != 15 { // 10 history + 5
		t.Errorf("got[0].Cumulative = %d, want 15", got[0].Cumulative)
	}
	if got[0].NewClusters != 5 {
		t.Errorf("got[0].NewClusters = %d, want 5", got[0].NewClusters)
	}
	if got[1].Month != "2026-02" || got[1].Cumulative != 18 {
		t.Errorf("got[1] = %+v, want 2026-02/cumulative 18", got[1])
	}
	if got[1].NewClusters != 3 {
		t.Errorf("got[1].NewClusters = %d, want 3", got[1].NewClusters)
	}

	osd0 := got[2]
	if osd0.ProductType != "OSD" || osd0.Month != "2026-01" || osd0.Cumulative != 2 {
		t.Errorf("got[2] = %+v, want OSD/2026-01/cumulative 2", osd0)
	}
	osd1 := got[3]
	if osd1.Month != "2026-02" || osd1.Cumulative != 2 || osd1.NewClusters != 0 {
		t.Errorf("got[3] = %+v, want OSD/2026-02/cumulative 2/new 0 (carried forward)", osd1)
	}
}
