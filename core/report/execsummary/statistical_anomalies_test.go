package execsummary

import (
	"fmt"
	"math"
	"testing"
)

func flatCostRecords(accountID string, monthCosts map[string]float64) []EnrichedCostRecord {
	var out []EnrichedCostRecord
	for month, cost := range monthCosts {
		out = append(out, EnrichedCostRecord{
			CostRecord:  CostRecord{Month: month, AccountID: accountID, Cost: cost},
			AccountName: accountID,
			ServiceName: "HCP",
		})
	}
	return out
}

func TestStatisticalAnomalies_DetectsSpike(t *testing.T) {
	// Prior months have just enough variance (sample stddev = 10) to clear
	// the $10 sigma floor, then a clear spike in the last month.
	enriched := flatCostRecords("111111111111", map[string]float64{
		"2026-02": 90,
		"2026-03": 100,
		"2026-04": 110,
		"2026-05": 500, // clear spike
	})
	got := StatisticalAnomalies(enriched, months("2026-02", "2026-03", "2026-04", "2026-05"))
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %+v", len(got), got)
	}
	a := got[0]
	if a.Direction != "spike" {
		t.Errorf("Direction = %q, want spike", a.Direction)
	}
	if a.AccountID != "111111111111" || a.Month != "2026-05" {
		t.Errorf("unexpected anomaly identity: %+v", a)
	}
	if a.CurrentCost != 500 || a.MeanCost != 100 {
		t.Errorf("CurrentCost/MeanCost = %v/%v, want 500/100", a.CurrentCost, a.MeanCost)
	}
	if a.ZScore <= 0 {
		t.Errorf("ZScore = %v, want positive for a spike", a.ZScore)
	}
}

func TestStatisticalAnomalies_DetectsDrop(t *testing.T) {
	enriched := flatCostRecords("111111111111", map[string]float64{
		"2026-02": 490,
		"2026-03": 500,
		"2026-04": 510,
		"2026-05": 50,
	})
	got := StatisticalAnomalies(enriched, months("2026-02", "2026-03", "2026-04", "2026-05"))
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %+v", len(got), got)
	}
	if got[0].Direction != "drop" {
		t.Errorf("Direction = %q, want drop", got[0].Direction)
	}
	if got[0].ZScore >= 0 {
		t.Errorf("ZScore = %v, want negative for a drop", got[0].ZScore)
	}
}

func TestStatisticalAnomalies_SkipsLowVarianceAccounts(t *testing.T) {
	// stddev of prior months is 0 (perfectly flat) -- below the $10 sigma floor.
	enriched := flatCostRecords("111111111111", map[string]float64{
		"2026-02": 100,
		"2026-03": 100,
		"2026-04": 100,
		"2026-05": 1000000, // would be a massive spike, but sigma floor blocks it
	})
	got := StatisticalAnomalies(enriched, months("2026-02", "2026-03", "2026-04", "2026-05"))
	if len(got) != 0 {
		t.Errorf("got = %+v, want no anomalies (sigma below $10 floor)", got)
	}
}

func TestStatisticalAnomalies_SkipsBelowZThreshold(t *testing.T) {
	enriched := flatCostRecords("111111111111", map[string]float64{
		"2026-02": 100,
		"2026-03": 150,
		"2026-04": 100,
		"2026-05": 120, // mild change, shouldn't clear |z| >= 2.0
	})
	got := StatisticalAnomalies(enriched, months("2026-02", "2026-03", "2026-04", "2026-05"))
	if len(got) != 0 {
		t.Errorf("got = %+v, want no anomalies below z threshold", got)
	}
}

func TestStatisticalAnomalies_RequiresAtLeastThreeMonths(t *testing.T) {
	enriched := flatCostRecords("111111111111", map[string]float64{
		"2026-04": 100,
		"2026-05": 500,
	})
	got := StatisticalAnomalies(enriched, months("2026-04", "2026-05"))
	if got != nil {
		t.Errorf("got = %+v, want nil for fewer than 3 months", got)
	}
}

func TestStatisticalAnomalies_RequiresTwoPriorMonthsPerAccount(t *testing.T) {
	// Account only has one prior month of data (2026-04); needs >= 2.
	enriched := flatCostRecords("111111111111", map[string]float64{
		"2026-04": 100,
		"2026-05": 500,
	})
	got := StatisticalAnomalies(enriched, months("2026-02", "2026-03", "2026-04", "2026-05"))
	if len(got) != 0 {
		t.Errorf("got = %+v, want no anomalies (only 1 prior month for this account)", got)
	}
}

func TestStatisticalAnomalies_SkipsAccountMissingLastMonth(t *testing.T) {
	enriched := flatCostRecords("111111111111", map[string]float64{
		"2026-02": 100,
		"2026-03": 120,
		"2026-04": 110,
		// no 2026-05 entry at all
	})
	got := StatisticalAnomalies(enriched, months("2026-02", "2026-03", "2026-04", "2026-05"))
	if len(got) != 0 {
		t.Errorf("got = %+v, want no anomalies (no current-month data)", got)
	}
}

func TestStatisticalAnomalies_SortsByAbsZScoreDescendingAndCapsAtFifteen(t *testing.T) {
	var enriched []EnrichedCostRecord
	// 20 accounts, each a clean spike but with increasing magnitude so
	// z-scores differ and are trivially ordered by account index.
	for i := 0; i < 20; i++ {
		id := accountIDFor(i)
		spike := 100.0 + float64(i+1)*1000 // bigger spike for higher i
		enriched = append(enriched, flatCostRecords(id, map[string]float64{
			// Slight variance (sample stddev = 10) clears the sigma floor.
			"2026-02": 90,
			"2026-03": 100,
			"2026-04": 110,
			"2026-05": spike,
		})...)
	}
	got := StatisticalAnomalies(enriched, months("2026-02", "2026-03", "2026-04", "2026-05"))
	if len(got) != 15 {
		t.Fatalf("len(got) = %d, want 15 (capped)", len(got))
	}
	for i := 1; i < len(got); i++ {
		if math.Abs(got[i-1].ZScore) < math.Abs(got[i].ZScore) {
			t.Errorf("anomalies not sorted by |z| descending at index %d: %v then %v", i, got[i-1].ZScore, got[i].ZScore)
		}
	}
	// The biggest spike (i=19) should be first.
	if got[0].AccountID != accountIDFor(19) {
		t.Errorf("got[0].AccountID = %q, want the largest-magnitude spike account", got[0].AccountID)
	}
}

func accountIDFor(i int) string {
	// Produce a distinct, valid-looking 12-digit test account ID per index.
	return fmt.Sprintf("1000000000%02d", i)
}

func TestMean(t *testing.T) {
	if got := mean(nil); got != 0 {
		t.Errorf("mean(nil) = %v, want 0", got)
	}
	if got := mean([]float64{1, 2, 3}); got != 2 {
		t.Errorf("mean([1,2,3]) = %v, want 2", got)
	}
}

func TestStddev(t *testing.T) {
	if got := stddev([]float64{5}, 5); got != 0 {
		t.Errorf("stddev(single value) = %v, want 0", got)
	}
	// Sample stddev of {2,4,4,4,5,5,7,9} is 2.138... (classic example).
	values := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	m := mean(values)
	got := stddev(values, m)
	if math.Abs(got-2.138) > 0.01 {
		t.Errorf("stddev = %v, want ~2.138", got)
	}
}
