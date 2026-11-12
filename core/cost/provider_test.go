// provider_test.go tests provider/group-by parsing and the Fetch entry point.
package cost

import "testing"

func TestFetchRequiresAccount(t *testing.T) {
	_, err := Fetch(t.Context(), CostQuery{Provider: ProviderAWS})
	if err == nil || err.Error() != "at least one account is required" {
		t.Fatalf("got %v", err)
	}
}

func TestMergeResultsSinglePassthrough(t *testing.T) {
	in := CostResult{Amount: 42, Currency: "USD", StartDate: "2026-04-25", EndDate: "2026-05-24"}
	out, err := MergeResults([]CostResult{in})
	if err != nil || out.Amount != 42 {
		t.Fatalf("got %v %v", out, err)
	}
}

func TestParseGroupBy(t *testing.T) {
	s, key, err := ParseGroupBy("service")
	if err != nil || s != GroupByService || key != "" {
		t.Fatalf("got %v %q %v", s, key, err)
	}
	s, key, err = ParseGroupBy("")
	if err != nil || s != GroupByNone || key != "" {
		t.Fatalf("got %v %q %v", s, key, err)
	}
	s, key, err = ParseGroupBy("account")
	if err != nil || s != GroupByAccount || key != "" {
		t.Fatalf("got %v %q %v", s, key, err)
	}
	s, key, err = ParseGroupBy("ou")
	if err != nil || s != GroupByOU || key != "" {
		t.Fatalf("got %v %q %v", s, key, err)
	}
	_, _, err = ParseGroupBy("region")
	if err == nil {
		t.Fatal("expected error")
	}
}
