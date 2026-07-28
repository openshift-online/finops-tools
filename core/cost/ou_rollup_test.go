package cost

import "testing"

func TestRollupBreakdownByOU(t *testing.T) {
	breakdown := []CostBreakdownItem{
		{Account: "111111111111", Amount: 10},
		{Account: "222222222222", Amount: 20},
		{Account: "333333333333", Amount: 5},
		{Account: "999999999999", Amount: 1},
	}
	buckets := map[string]OUBucket{
		"111111111111": {ID: "ou-root-prod", Name: "Production"},
		"222222222222": {ID: "ou-root-prod", Name: "Production"},
		"333333333333": {ID: "ou-root-sandbox", Name: "Sandbox"},
	}

	got := RollupBreakdownByOU(breakdown, buckets)
	if len(got) != 3 {
		t.Fatalf("len = %d, got %+v", len(got), got)
	}
	if got[0].OUID != "ou-root-prod" || got[0].Amount != 30 || got[0].OUName != "Production" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].OUID != "ou-root-sandbox" || got[1].Amount != 5 {
		t.Fatalf("second = %+v", got[1])
	}
	if got[2].OUID != unknownOUBucketID || got[2].Amount != 1 {
		t.Fatalf("unknown = %+v", got[2])
	}
}

func TestRollupBreakdownByOUTree(t *testing.T) {
	breakdown := []CostBreakdownItem{
		{Account: "111111111111", Amount: 10}, // direct under Production
		{Account: "333333333333", Amount: 5},  // under Team A
		{Account: "444444444444", Amount: 2},  // under Sandbox
	}
	parents := map[string]OUBucket{
		"111111111111": {ID: "ou-root-prod", Name: "Production"},
		"333333333333": {ID: "ou-prod-team-a", Name: "Team A"},
		"444444444444": {ID: "ou-root-sandbox", Name: "Sandbox"},
	}
	// Hierarchy lists Sandbox before Production; siblings should still emit by amount desc.
	hierarchy := []OUHierarchyNode{
		{ID: "r-root", Name: "Root", Depth: 0},
		{ID: "ou-root-sandbox", Name: "Sandbox", ParentID: "r-root", Depth: 1},
		{ID: "ou-root-prod", Name: "Production", ParentID: "r-root", Depth: 1},
		{ID: "ou-prod-team-a", Name: "Team A", ParentID: "ou-root-prod", Depth: 2},
	}

	got := RollupBreakdownByOUTree(breakdown, parents, hierarchy)
	if len(got) != 4 {
		t.Fatalf("len = %d, got %+v", len(got), got)
	}
	byID := map[string]CostBreakdownItem{}
	for _, row := range got {
		byID[row.OUID] = row
	}
	if byID["r-root"].Amount != 17 || byID["r-root"].OUDepth != 0 {
		t.Fatalf("root = %+v", byID["r-root"])
	}
	if byID["ou-root-prod"].Amount != 15 || byID["ou-root-prod"].OUDepth != 1 {
		t.Fatalf("prod = %+v", byID["ou-root-prod"])
	}
	if byID["ou-prod-team-a"].Amount != 5 || byID["ou-prod-team-a"].OUDepth != 2 {
		t.Fatalf("team-a = %+v", byID["ou-prod-team-a"])
	}
	if byID["ou-root-sandbox"].Amount != 2 {
		t.Fatalf("sandbox = %+v", byID["ou-root-sandbox"])
	}
	// DFS with siblings ordered by amount desc: Root → Production(15) → Team A → Sandbox(2)
	if got[0].OUID != "r-root" || got[1].OUID != "ou-root-prod" || got[2].OUID != "ou-prod-team-a" || got[3].OUID != "ou-root-sandbox" {
		t.Fatalf("order = %+v", got)
	}
}

func TestParseSplitByOU(t *testing.T) {
	s, err := ParseSplitBy("ou")
	if err != nil || s != SplitByOU {
		t.Fatalf("got %q err %v", s, err)
	}
}
