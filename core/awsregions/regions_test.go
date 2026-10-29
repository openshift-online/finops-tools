package awsregions

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	input := []string{" us-west-2 ", "", "us-east-1", "us-west-2"}
	before := append([]string(nil), input...)
	if got := Normalize(input); !reflect.DeepEqual(got, []string{"us-east-1", "us-west-2"}) {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("normalization changed the caller's input")
	}
}

func TestExclude(t *testing.T) {
	for _, tc := range []struct {
		name string
		skip []string
		want []string
	}{
		{name: "no exclusions", want: []string{"us-east-1", "us-west-2"}},
		{name: "duplicates and whitespace", skip: []string{" us-west-2 ", "us-west-2"}, want: []string{"us-east-1"}},
		{name: "region absent from account", skip: []string{"eu-west-1"}, want: []string{"us-east-1", "us-west-2"}},
		{name: "all excluded", skip: []string{"us-east-1", "us-west-2"}, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			regions := []string{"us-east-1", "us-west-2"}
			if got := Exclude(regions, tc.skip); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("regions = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(regions, []string{"us-east-1", "us-west-2"}) {
				t.Fatal("exclusion changed the caller's input")
			}
		})
	}
}
