package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/cost"
)

type fakeBillingAPI struct {
	dimensions func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error)
	usage      func(context.Context, *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error)
}

func (f fakeBillingAPI) GetDimensionValues(ctx context.Context, input *costexplorer.GetDimensionValuesInput, _ ...func(*costexplorer.Options)) (*costexplorer.GetDimensionValuesOutput, error) {
	return f.dimensions(ctx, input)
}
func (f fakeBillingAPI) GetCostAndUsage(ctx context.Context, input *costexplorer.GetCostAndUsageInput, _ ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	return f.usage(ctx, input)
}

func billingTestPeriod() cost.DateRange {
	return cost.DateRange{Start: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
}

func billingDimensions(values ...string) []cetypes.DimensionValuesWithAttributes {
	out := make([]cetypes.DimensionValuesWithAttributes, 0, len(values))
	for _, value := range values {
		out = append(out, cetypes.DimensionValuesWithAttributes{Value: aws.String(value)})
	}
	return out
}

func TestBillingCandidatesPaginateAndBatchExactUsageTypes(t *testing.T) {
	usageTypes := []string{"USE1-AmazonEKS-Hours:extendedSupport", "EU-RDS:ExtendedSupport:Yr1-Yr2"}
	dimensionPages, accountPages := 0, 0
	api := fakeBillingAPI{
		dimensions: func(_ context.Context, input *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
			dimensionPages++
			if input.Dimension != cetypes.DimensionUsageType || input.Context != cetypes.ContextCostAndUsage || input.SearchString != nil || aws.ToString(input.TimePeriod.Start) != "2026-09-20" || aws.ToString(input.TimePeriod.End) != "2026-10-20" {
				t.Errorf("dimension request = %+v", input)
			}
			switch aws.ToString(input.NextPageToken) {
			case "":
				return &costexplorer.GetDimensionValuesOutput{NextPageToken: aws.String("empty")}, nil
			case "empty":
				return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions(usageTypes[0], "BoxUsage:m5.large"), NextPageToken: aws.String("last")}, nil
			default:
				return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions(usageTypes[1], usageTypes[0], "UPPER-EXTENDEDSUPPORT", "ordinary")}, nil
			}
		},
		usage: func(_ context.Context, input *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
			accountPages++
			want := []string{usageTypes[1], "UPPER-EXTENDEDSUPPORT", usageTypes[0]}
			if input.Filter == nil || input.Filter.Dimensions.Key != cetypes.DimensionUsageType || !reflect.DeepEqual(input.Filter.Dimensions.Values, want) || !reflect.DeepEqual(input.Filter.Dimensions.MatchOptions, []cetypes.MatchOption{cetypes.MatchOptionEquals}) || len(input.GroupBy) != 1 || input.GroupBy[0].Type != cetypes.GroupDefinitionTypeDimension || aws.ToString(input.GroupBy[0].Key) != "LINKED_ACCOUNT" || input.Granularity != cetypes.GranularityMonthly || aws.ToString(input.TimePeriod.Start) != "2026-09-20" {
				t.Errorf("usage request = %+v", input)
			}
			if input.NextPageToken == nil {
				return &costexplorer.GetCostAndUsageOutput{NextPageToken: aws.String("next"), ResultsByTime: []cetypes.ResultByTime{
					{Groups: []cetypes.Group{{Keys: []string{"111111111111"}, Metrics: map[string]cetypes.MetricValue{"UnblendedCost": {Amount: aws.String("0")}}}}},
					{Groups: []cetypes.Group{{Keys: []string{"222222222222"}, Metrics: map[string]cetypes.MetricValue{"UnblendedCost": {Amount: aws.String("-5")}}}}},
				}}, nil
			}
			return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{{Keys: []string{"111111111111"}}, {Keys: []string{"333333333333"}}}}}}, nil
		},
	}
	result, err := discoverBillingCandidatesWithClient(context.Background(), api, billingTestPeriod())
	if err != nil {
		t.Fatal(err)
	}
	if dimensionPages != 3 || accountPages != 2 || !reflect.DeepEqual(result.AccountIDs, []string{"111111111111", "222222222222", "333333333333"}) || len(result.UsageTypes) != 3 {
		t.Fatalf("result=%+v dimensionPages=%d accountPages=%d", result, dimensionPages, accountPages)
	}
}

func TestBillingCandidatesSplitLargeUsageTypeSets(t *testing.T) {
	var values []string
	for i := 0; i < billingUsageTypeBatchSize+1; i++ {
		values = append(values, fmt.Sprintf("ExtendedSupport-%03d", i))
	}
	var batches []int
	result, err := discoverBillingCandidatesWithClient(context.Background(), fakeBillingAPI{
		dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
			return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions(values...)}, nil
		},
		usage: func(_ context.Context, input *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
			batches = append(batches, len(input.Filter.Dimensions.Values))
			return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{{Keys: []string{"111111111111"}}}}}}, nil
		},
	}, billingTestPeriod())
	if err != nil || !reflect.DeepEqual(batches, []int{billingUsageTypeBatchSize, 1}) || len(result.AccountIDs) != 1 {
		t.Fatalf("result=%+v batches=%v error=%v", result, batches, err)
	}
}

func TestBillingCandidatesEmptyUsageTypesDoNotQueryAccounts(t *testing.T) {
	result, err := discoverBillingCandidatesWithClient(context.Background(), fakeBillingAPI{
		dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
			return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ordinary")}, nil
		},
		usage: func(context.Context, *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
			t.Error("unfiltered account query was made")
			return nil, nil
		},
	}, billingTestPeriod())
	if err != nil || len(result.AccountIDs) != 0 || len(result.UsageTypes) != 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestBillingCandidatesFailWithoutPartialSelection(t *testing.T) {
	for _, stage := range []string{"dimensions", "dimension page", "nil dimensions", "dimension cycle", "usage", "usage page", "nil usage", "usage cycle", "invalid group", "cancellation"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("AccessDenied")
			api := fakeBillingAPI{
				dimensions: func(_ context.Context, input *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
					if stage == "dimensions" || (stage == "dimension page" && input.NextPageToken != nil) {
						return nil, failure
					}
					if stage == "nil dimensions" {
						return nil, nil
					}
					if stage == "cancellation" {
						cancel()
						return nil, ctx.Err()
					}
					out := &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("RDS:ExtendedSupport")}
					if stage == "dimension cycle" || (stage == "dimension page" && input.NextPageToken == nil) {
						out.NextPageToken = aws.String("next")
					}
					return out, nil
				},
				usage: func(_ context.Context, input *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
					if stage == "usage" || (stage == "usage page" && input.NextPageToken != nil) {
						return nil, failure
					}
					if stage == "nil usage" {
						return nil, nil
					}
					key := "111111111111"
					if stage == "invalid group" {
						key = "not-an-account"
					}
					out := &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{{Keys: []string{key}}}}}}
					if stage == "usage cycle" || (stage == "usage page" && input.NextPageToken == nil) {
						out.NextPageToken = aws.String("next")
					}
					return out, nil
				},
			}
			result, err := discoverBillingCandidatesWithClient(ctx, api, billingTestPeriod())
			if err == nil || len(result.AccountIDs) != 0 || len(result.UsageTypes) != 0 {
				t.Fatalf("partial result=%+v error=%v", result, err)
			}
			if stage == "cancellation" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if strings.Contains(stage, "cycle") && !strings.Contains(err.Error(), "repeated pagination token") {
				t.Fatal(err)
			}
		})
	}
}

func TestBillingLookbackRangeUsesUTCCalendarDays(t *testing.T) {
	now := time.Date(2026, 10, 19, 23, 30, 0, 0, time.FixedZone("Eastern", -4*60*60))
	for _, tc := range []struct {
		days       int
		start, end string
	}{{30, "2026-09-21", "2026-10-21"}, {1, "2026-10-20", "2026-10-21"}} {
		period, err := BillingLookbackRange(now, tc.days)
		if err != nil || period.Start.Format("2006-01-02") != tc.start || period.End.Format("2006-01-02") != tc.end {
			t.Fatalf("days=%d period=%+v error=%v", tc.days, period, err)
		}
	}
	if _, err := BillingLookbackRange(now, 0); err == nil {
		t.Fatal("zero-day lookback accepted")
	}
	if _, err := discoverBillingCandidatesWithClient(context.Background(), fakeBillingAPI{}, cost.DateRange{}); err == nil {
		t.Fatal("empty date range accepted")
	}
}
