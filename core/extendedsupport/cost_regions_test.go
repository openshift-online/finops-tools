package extendedsupport

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/openshift-online/finops-tools/core/cost"
)

func TestSupportCostsRegionFiltersApplyToEveryPageServiceAndAccountBatch(t *testing.T) {
	for _, tc := range []struct {
		name          string
		regions, skip []string
		want          *cetypes.Expression
		empty         bool
	}{
		{name: "unrestricted"},
		{name: "include normalized", regions: []string{" us-west-2 ", "us-east-1", "us-east-1"},
			want: &cetypes.Expression{Dimensions: &cetypes.DimensionValues{Key: cetypes.DimensionRegion, Values: []string{"us-east-1", "us-west-2"}}}},
		{name: "exclude only", skip: []string{" us-west-2 ", "us-west-2"},
			want: &cetypes.Expression{Not: &cetypes.Expression{Dimensions: &cetypes.DimensionValues{Key: cetypes.DimensionRegion, Values: []string{"us-west-2"}}}}},
		{name: "exclusions win", regions: []string{"us-west-2", "us-east-1"}, skip: []string{"us-west-2", "eu-west-1"},
			want: &cetypes.Expression{Dimensions: &cetypes.DimensionValues{Key: cetypes.DimensionRegion, Values: []string{"us-east-1"}}}},
		{name: "all explicitly included regions excluded", regions: []string{" us-east-1 ", "us-east-1"}, skip: []string{"us-east-1", "us-west-2"}, empty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dimensions, requests := 0, 0
			q := supportCostQuery(fakeBillingAPI{
				dimensions: func(_ context.Context, in *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
					dimensions++
					if in.Filter != nil {
						t.Fatal("usage-type metadata lookup must remain global")
					}
					return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
				},
				usage: func(_ context.Context, in *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
					requests++
					wantFilters := 3
					if tc.want != nil {
						wantFilters++
					}
					if len(in.Filter.And) != wantFilters {
						t.Fatalf("filter=%+v", in.Filter)
					}
					if tc.want != nil && !reflect.DeepEqual(in.Filter.And[3], *tc.want) {
						t.Fatalf("region filter=%+v, want=%+v", in.Filter.And[3], tc.want)
					}
					id := in.Filter.And[2].Dimensions.Values[0]
					out := &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{supportCostBucket(supportCostGroup(id, "2", "USD"))}}
					if in.NextPageToken == nil {
						out.NextPageToken = aws.String("next")
					} else {
						out.ResultsByTime[0].Groups[0] = supportCostGroup(id, "3", "USD")
					}
					return out, nil
				},
			})
			q.Regions, q.SkipRegions = tc.regions, tc.skip
			q.Services = []Service{ServiceRDS, ServiceEKS}
			q.Targets = nil
			for i := 0; i < 101; i++ {
				q.Targets = append(q.Targets, cost.AccountTarget{AccountID: fmt.Sprintf("%012d", i), PayerAccountID: "123456789012"})
			}
			result, err := FetchCosts(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Regions, awsregions.Normalize(tc.regions)) || !reflect.DeepEqual(result.ExcludedRegions, awsregions.Normalize(tc.skip)) {
				t.Fatalf("lost region scope: %+v", result)
			}
			if tc.empty {
				if dimensions != 0 || requests != 0 || result.Amount != 0 || len(result.Rows) != 0 || result.Rows == nil {
					t.Fatalf("empty scope queried billing: result=%+v dimensions=%d requests=%d", result, dimensions, requests)
				}
			} else if dimensions != 1 || requests != 8 || result.Amount != 20 {
				t.Fatalf("result=%+v dimensions=%d requests=%d", result, dimensions, requests)
			}
		})
	}
}
