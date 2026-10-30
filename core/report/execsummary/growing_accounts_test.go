package execsummary

import (
	"testing"
	"time"
)

func months(labels ...string) []time.Time {
	out := make([]time.Time, len(labels))
	for i, l := range labels {
		t, err := time.Parse("2006-01", l)
		if err != nil {
			panic(err)
		}
		out[i] = t
	}
	return out
}

func TestTopGrowingAccounts_RanksByDeltaDescending(t *testing.T) {
	enriched := []EnrichedCostRecord{
		{CostRecord: CostRecord{Month: "2026-04", AccountID: "111111111111", Cost: 100}, AccountName: "A", ServiceName: "HCP"},
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "111111111111", Cost: 150}, AccountName: "A", ServiceName: "HCP"},
		{CostRecord: CostRecord{Month: "2026-04", AccountID: "222222222222", Cost: 500}, AccountName: "B", ServiceName: "Tooling"},
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "222222222222", Cost: 400}, AccountName: "B", ServiceName: "Tooling"},
		{CostRecord: CostRecord{Month: "2026-04", AccountID: "333333333333", Cost: 10}, AccountName: "C", ServiceName: "Unmapped"},
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "333333333333", Cost: 90}, AccountName: "C", ServiceName: "Unmapped"},
	}

	got := TopGrowingAccounts(enriched, months("2026-04", "2026-05"), 5, 3)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3: %+v", len(got), got)
	}
	// Account C has the biggest delta (+80), then A (+50), then B (-100).
	if got[0].AccountID != "333333333333" || got[0].Delta != 80 {
		t.Errorf("got[0] = %+v, want account 333333333333 delta 80", got[0])
	}
	if got[1].AccountID != "111111111111" || got[1].Delta != 50 {
		t.Errorf("got[1] = %+v, want account 111111111111 delta 50", got[1])
	}
	if got[2].AccountID != "222222222222" || got[2].Delta != -100 {
		t.Errorf("got[2] = %+v, want account 222222222222 delta -100", got[2])
	}
}

func TestTopGrowingAccounts_LimitsToTopN(t *testing.T) {
	var enriched []EnrichedCostRecord
	ids := []string{"111111111111", "222222222222", "333333333333"}
	deltas := []float64{10, 20, 30}
	for i, id := range ids {
		enriched = append(enriched,
			EnrichedCostRecord{CostRecord: CostRecord{Month: "2026-04", AccountID: id, Cost: 0}, AccountName: id},
			EnrichedCostRecord{CostRecord: CostRecord{Month: "2026-05", AccountID: id, Cost: deltas[i]}, AccountName: id},
		)
	}
	got := TopGrowingAccounts(enriched, months("2026-04", "2026-05"), 2, 3)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].AccountID != "333333333333" || got[1].AccountID != "222222222222" {
		t.Errorf("unexpected top-2 order: %+v", got)
	}
}

func TestTopGrowingAccounts_TreatsMissingMonthAsZero(t *testing.T) {
	// Account only has data in the last month -- prev month cost should be
	// treated as 0, not cause a panic or be excluded.
	enriched := []EnrichedCostRecord{
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "111111111111", Cost: 42}, AccountName: "A"},
	}
	got := TopGrowingAccounts(enriched, months("2026-04", "2026-05"), 5, 3)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].PrevMonthCost != 0 || got[0].LastMonthCost != 42 || got[0].Delta != 42 {
		t.Errorf("got[0] = %+v, want PrevMonthCost=0 LastMonthCost=42 Delta=42", got[0])
	}
}

func TestTopGrowingAccounts_FewerThanTwoMonths(t *testing.T) {
	enriched := []EnrichedCostRecord{
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "111111111111", Cost: 42}},
	}
	if got := TopGrowingAccounts(enriched, months("2026-05"), 5, 3); got != nil {
		t.Errorf("got = %+v, want nil for fewer than 2 months", got)
	}
}

func TestTopGrowingAccounts_NoData(t *testing.T) {
	if got := TopGrowingAccounts(nil, months("2026-04", "2026-05"), 5, 3); got != nil {
		t.Errorf("got = %+v, want nil for no data", got)
	}
}

func TestTopGrowingAccounts_IncludesTopServices(t *testing.T) {
	enriched := []EnrichedCostRecord{
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "111111111111", Service: "EC2", Cost: 100}, AccountName: "A"},
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "111111111111", Service: "S3", Cost: 50}, AccountName: "A"},
		{CostRecord: CostRecord{Month: "2026-04", AccountID: "111111111111", Service: "EC2", Cost: 10}, AccountName: "A"},
	}
	got := TopGrowingAccounts(enriched, months("2026-04", "2026-05"), 5, 2)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0].TopServices) != 2 {
		t.Fatalf("len(TopServices) = %d, want 2: %+v", len(got[0].TopServices), got[0].TopServices)
	}
	if got[0].TopServices[0].Service != "EC2" || got[0].TopServices[0].Cost != 100 {
		t.Errorf("TopServices[0] = %+v, want EC2 cost 100", got[0].TopServices[0])
	}
}

func TestTopServicesForAccount_SortsDescendingAndLimits(t *testing.T) {
	enriched := []EnrichedCostRecord{
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "A", Service: "EC2", Cost: 10}},
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "A", Service: "S3", Cost: 30}},
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "A", Service: "RDS", Cost: 20}},
		{CostRecord: CostRecord{Month: "2026-04", AccountID: "A", Service: "Lambda", Cost: 999}}, // wrong month, excluded
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "B", Service: "EC2", Cost: 999}},    // wrong account, excluded
	}
	got := TopServicesForAccount(enriched, "A", "2026-05", 2)
	want := []ServiceCostPair{{Service: "S3", Cost: 30}, {Service: "RDS", Cost: 20}}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTopServicesForAccount_AggregatesSameServiceAcrossRows(t *testing.T) {
	enriched := []EnrichedCostRecord{
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "A", Service: "EC2", Cost: 10}},
		{CostRecord: CostRecord{Month: "2026-05", AccountID: "A", Service: "EC2", Cost: 5}},
	}
	got := TopServicesForAccount(enriched, "A", "2026-05", 5)
	if len(got) != 1 || got[0].Cost != 15 {
		t.Errorf("got = %+v, want single EC2 row summed to 15", got)
	}
}

func TestTopServicesForAccount_NoMatches(t *testing.T) {
	got := TopServicesForAccount(nil, "A", "2026-05", 3)
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}
