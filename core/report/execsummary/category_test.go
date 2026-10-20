package execsummary

import "testing"

func TestEnrichCostRecords_MapsAndFallsBack(t *testing.T) {
	mapping := AccountTaxonomyMap{
		"111111111111": {AccountID: "111111111111", AccountName: "Payer Control", ServiceName: "HCP"},
	}
	records := []CostRecord{
		{Month: "2026-05", AccountID: "111111111111", Cost: 100, Service: "EC2"},
		{Month: "2026-05", AccountID: "999999999999", Cost: 50, Service: "S3"},
	}

	got := EnrichCostRecords(records, mapping)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].ServiceName != "HCP" || got[0].AccountName != "Payer Control" {
		t.Errorf("got[0] = %+v, want mapped ServiceName=HCP AccountName=Payer Control", got[0])
	}
	if got[1].ServiceName != UnmappedServiceName {
		t.Errorf("got[1].ServiceName = %q, want %q", got[1].ServiceName, UnmappedServiceName)
	}
	if got[1].AccountName != "999999999999" {
		t.Errorf("got[1].AccountName = %q, want raw account ID fallback", got[1].AccountName)
	}
	// Original CostRecord fields must be preserved.
	if got[0].Cost != 100 || got[0].Service != "EC2" {
		t.Errorf("got[0] lost CostRecord fields: %+v", got[0])
	}
}

func TestCategoryMonthly_AggregatesByServiceAndMonth(t *testing.T) {
	enriched := []EnrichedCostRecord{
		{CostRecord: CostRecord{Month: "2026-05", Cost: 100}, ServiceName: "HCP"},
		{CostRecord: CostRecord{Month: "2026-05", Cost: 50}, ServiceName: "HCP"},
		{CostRecord: CostRecord{Month: "2026-05", Cost: 25}, ServiceName: "Tooling"},
		{CostRecord: CostRecord{Month: "2026-04", Cost: 10}, ServiceName: "HCP"},
	}

	got := CategoryMonthly(enriched)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3: %+v", len(got), got)
	}
	// Sorted by month then service name.
	want := []CategoryMonthRecord{
		{ServiceName: "HCP", Month: "2026-04", Cost: 10},
		{ServiceName: "HCP", Month: "2026-05", Cost: 150},
		{ServiceName: "Tooling", Month: "2026-05", Cost: 25},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestCategoryMonthly_EmptyInput(t *testing.T) {
	got := CategoryMonthly(nil)
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestCategoryMonthly_UnmappedFallbackAppearsAsOwnBucket(t *testing.T) {
	enriched := []EnrichedCostRecord{
		{CostRecord: CostRecord{Month: "2026-05", Cost: 30}, ServiceName: UnmappedServiceName},
	}
	got := CategoryMonthly(enriched)
	if len(got) != 1 || got[0].ServiceName != UnmappedServiceName || got[0].Cost != 30 {
		t.Errorf("got = %+v, want single Unmapped bucket with cost 30", got)
	}
}
