package extendedsupport

import (
	"reflect"
	"testing"
)

func TestParseServices(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    []Service
		wantErr bool
	}{
		{name: "empty selects default", value: "", want: []Service{ServiceRDS}},
		{name: "rds", value: "rds", want: []Service{ServiceRDS}},
		{name: "case insensitive", value: " RDS ", want: []Service{ServiceRDS}},
		{name: "deduplicates", value: "rds,rds", want: []Service{ServiceRDS}},
		{name: "unknown", value: "eks", wantErr: true},
		{name: "mixed unknown", value: "rds,eks", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseServices(tc.value)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseServices(%q) error = nil, want error", tc.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseServices(%q) error = %v", tc.value, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseServices(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestParseRegions(t *testing.T) {
	got, err := ParseRegions("us-east-1, us-west-2")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"us-east-1", "us-west-2"}) {
		t.Fatalf("ParseRegions() = %v", got)
	}

	if got, err := ParseRegions(""); err != nil || got != nil {
		t.Fatalf("ParseRegions(\"\") = %v, %v; want nil, nil", got, err)
	}
}
