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
		"111111111111": {ID: "ou-root-prod0000", Name: "Production"},
		"222222222222": {ID: "ou-root-prod0000", Name: "Production"},
		"333333333333": {ID: "ou-root-sandbox0", Name: "Sandbox"},
	}

	got := RollupBreakdownByOU(breakdown, buckets)
	if len(got) != 3 {
		t.Fatalf("len = %d, got %+v", len(got), got)
	}
	if got[0].OUID != "ou-root-prod0000" || got[0].Amount != 30 || got[0].OUName != "Production" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].OUID != "ou-root-sandbox0" || got[1].Amount != 5 {
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
		"111111111111": {ID: "ou-root-prod0000", Name: "Production"},
		"333333333333": {ID: "ou-prod-teama000", Name: "Team A"},
		"444444444444": {ID: "ou-root-sandbox0", Name: "Sandbox"},
	}
	// Hierarchy lists Sandbox before Production; siblings should still emit by amount desc.
	hierarchy := []OUHierarchyNode{
		{ID: "r-root", Name: "Root", Depth: 0},
		{ID: "ou-root-sandbox0", Name: "Sandbox", ParentID: "r-root", Depth: 1},
		{ID: "ou-root-prod0000", Name: "Production", ParentID: "r-root", Depth: 1},
		{ID: "ou-prod-teama000", Name: "Team A", ParentID: "ou-root-prod0000", Depth: 2},
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
	if byID["ou-root-prod0000"].Amount != 15 || byID["ou-root-prod0000"].OUDepth != 1 {
		t.Fatalf("prod = %+v", byID["ou-root-prod0000"])
	}
	if byID["ou-prod-teama000"].Amount != 5 || byID["ou-prod-teama000"].OUDepth != 2 {
		t.Fatalf("team-a = %+v", byID["ou-prod-teama000"])
	}
	if byID["ou-root-sandbox0"].Amount != 2 {
		t.Fatalf("sandbox = %+v", byID["ou-root-sandbox0"])
	}
	// DFS with siblings ordered by amount desc: Root → Production(15) → Team A → Sandbox(2)
	if got[0].OUID != "r-root" || got[1].OUID != "ou-root-prod0000" || got[2].OUID != "ou-prod-teama000" || got[3].OUID != "ou-root-sandbox0" {
		t.Fatalf("order = %+v", got)
	}
}

func TestRollupBreakdownByOUTreeOrphanParent(t *testing.T) {
	breakdown := []CostBreakdownItem{
		{Account: "111111111111", Amount: 10},
		{Account: "222222222222", Amount: 3}, // parent OU missing from hierarchy
	}
	parents := map[string]OUBucket{
		"111111111111": {ID: "ou-root-prod0000", Name: "Production"},
		"222222222222": {ID: "ou-missing-xxxxx", Name: "Gone"},
	}
	hierarchy := []OUHierarchyNode{
		{ID: "r-root", Name: "Root", Depth: 0},
		{ID: "ou-root-prod0000", Name: "Production", ParentID: "r-root", Depth: 1},
	}

	got := RollupBreakdownByOUTree(breakdown, parents, hierarchy)
	byID := map[string]CostBreakdownItem{}
	for _, row := range got {
		byID[row.OUID] = row
	}
	if byID["r-root"].Amount != 10 {
		t.Fatalf("root = %+v", byID["r-root"])
	}
	if byID[unknownOUBucketID].Amount != 3 {
		t.Fatalf("unknown = %+v", byID[unknownOUBucketID])
	}
	var total float64
	for _, row := range got {
		if row.OUDepth == 0 || row.OUID == unknownOUBucketID {
			total += row.Amount
		}
	}
	if total != 13 {
		t.Fatalf("report total = %v, want 13; got %+v", total, got)
	}
}

func TestParseGroupByOU(t *testing.T) {
	s, err := ParseGroupBy("ou")
	if err != nil || s != GroupByOU {
		t.Fatalf("got %q err %v", s, err)
	}
}

func TestFormatAccountOUPaths(t *testing.T) {
	parents := map[string]OUBucket{
		"111111111111": {ID: "ou-root-team0000", Name: "Team A"},
		"222222222222": {ID: "ou-root-prod0000", Name: "Production"},
	}
	hierarchy := []OUHierarchyNode{
		{ID: "r-xxxx", Name: "Root", Depth: 0},
		{ID: "ou-root-prod0000", Name: "Production", ParentID: "r-xxxx", Depth: 1},
		{ID: "ou-root-team0000", Name: "Team A", ParentID: "ou-root-prod0000", Depth: 2},
	}
	got := FormatAccountOUPaths(parents, hierarchy)
	if got["111111111111"] != "Root / Production / Team A" {
		t.Fatalf("team path = %q", got["111111111111"])
	}
	if got["222222222222"] != "Root / Production" {
		t.Fatalf("prod path = %q", got["222222222222"])
	}
}

func TestFormatAccountOUPathsMissingAncestorUsesID(t *testing.T) {
	parents := map[string]OUBucket{
		"111111111111": {ID: "ou-root-team0000", Name: "Team A"},
	}
	hierarchy := []OUHierarchyNode{
		{ID: "ou-root-team0000", Name: "Team A", ParentID: "ou-missing-parent", Depth: 2},
	}
	got := FormatAccountOUPaths(parents, hierarchy)
	if got["111111111111"] != "ou-missing-parent / Team A" {
		t.Fatalf("path = %q", got["111111111111"])
	}
}
