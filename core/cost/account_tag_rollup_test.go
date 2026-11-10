package cost

import "testing"

func TestRollupBreakdownByAccountTag(t *testing.T) {
	breakdown := []CostBreakdownItem{
		{Account: "111111111111", Amount: 10},
		{Account: "222222222222", Amount: 20},
		{Account: "333333333333", Amount: 5},
		{Account: "999999999999", Amount: 1},
	}
	values := map[string]string{
		"111111111111": "Hybrid Platform",
		"222222222222": "Hybrid Platform",
		"333333333333": "",
	}

	got := RollupBreakdownByAccountTag(breakdown, values)
	if len(got) != 3 {
		t.Fatalf("len = %d, got %+v", len(got), got)
	}
	if got[0].TagValueString() != "Hybrid Platform" || got[0].Amount != 30 {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].TagValue == nil || got[1].TagValueString() != "" || got[1].Amount != 5 {
		t.Fatalf("empty = %+v", got[1])
	}
	if got[2].TagValueString() != untaggedAccountTagValue || got[2].Amount != 1 {
		t.Fatalf("untagged = %+v", got[2])
	}
}

func TestParseGroupByAccountTag(t *testing.T) {
	mode, key, err := ParseGroupBy("account-tag:organization")
	if err != nil || mode != GroupByAccountTag || key != "organization" {
		t.Fatalf("got %q %q %v", mode, key, err)
	}
	mode, key, err = ParseGroupBy("Account-Tag:Service-Name")
	if err != nil || mode != GroupByAccountTag || key != "Service-Name" {
		t.Fatalf("got %q %q %v", mode, key, err)
	}
	_, _, err = ParseGroupBy("account-tag")
	if err == nil {
		t.Fatal("expected error for bare account-tag")
	}
	_, _, err = ParseGroupBy("account-tag:")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	_, _, err = ParseGroupBy("tag:organization")
	if err == nil {
		t.Fatal("expected error for bare tag: prefix")
	}
}
