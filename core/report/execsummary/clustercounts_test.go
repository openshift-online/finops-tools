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
