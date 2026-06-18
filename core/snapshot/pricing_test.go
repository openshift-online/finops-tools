package snapshot

import "testing"

func TestEstimateMonthlyCost(t *testing.T) {
	tests := []struct {
		kind   Kind
		size   float64
		tier   string
		want   float64
	}{
		{KindEBSSnapshot, 100, "standard", 5},
		{KindEBSSnapshot, 100, "archive", 1.25},
		{KindRDSSnapshot, 200, "", 19},
		{KindRDSClusterSnapshot, 50, "", 4.75},
		{KindEBSSnapshot, 0, "standard", 0},
	}
	for _, tt := range tests {
		got := EstimateMonthlyCost(tt.kind, tt.size, tt.tier, "us-east-1")
		if got != tt.want {
			t.Errorf("EstimateMonthlyCost(%q, %v, %q) = %v, want %v", tt.kind, tt.size, tt.tier, got, tt.want)
		}
	}
}

func TestParseTypes(t *testing.T) {
	all, err := ParseTypes("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("default types = %d, want 3", len(all))
	}

	ebsOnly, err := ParseTypes("ebs")
	if err != nil {
		t.Fatal(err)
	}
	if len(ebsOnly) != 1 || ebsOnly[0] != KindEBSSnapshot {
		t.Fatalf("ebs only = %#v", ebsOnly)
	}

	rdsKinds, err := ParseTypes("rds")
	if err != nil {
		t.Fatal(err)
	}
	if len(rdsKinds) != 2 {
		t.Fatalf("rds kinds = %d, want 2", len(rdsKinds))
	}

	if _, err := ParseTypes("unknown"); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestParseRegions(t *testing.T) {
	regions, err := ParseRegions("")
	if err != nil || regions != nil {
		t.Fatalf("empty regions = %#v, err = %v", regions, err)
	}
	regions, err = ParseRegions("us-east-1, eu-west-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 || regions[0] != "us-east-1" {
		t.Fatalf("regions = %#v", regions)
	}
}
