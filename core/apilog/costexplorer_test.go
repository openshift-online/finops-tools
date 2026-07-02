package apilog

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

func TestFormatGetCostAndUsage(t *testing.T) {
	got := formatGetCostAndUsage(&costexplorer.GetCostAndUsageInput{
		TimePeriod: &types.DateInterval{
			Start: aws.String("2026-06-01"),
			End:   aws.String("2026-07-01"),
		},
		Granularity: types.GranularityDaily,
		Metrics:     []string{"NetAmortizedCost"},
		GroupBy: []types.GroupDefinition{{
			Type: types.GroupDefinitionTypeDimension,
			Key:  aws.String("SERVICE"),
		}},
		Filter: &types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionLinkedAccount,
				Values: []string{"111111111111"},
			},
		},
	})
	for _, want := range []string{
		"CostExplorer.GetCostAndUsage",
		"start=2026-06-01",
		"end=2026-07-01",
		"granularity=DAILY",
		"metrics=NetAmortizedCost",
		"groupBy=SERVICE",
		"filter=LINKED_ACCOUNT=111111111111",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatGetCostAndUsage() = %q, missing %q", got, want)
		}
	}
}

func TestFormatCEFilterOr(t *testing.T) {
	got := formatCEFilter(&types.Expression{
		Or: []types.Expression{
			{
				Dimensions: &types.DimensionValues{
					Key:    types.DimensionService,
					Values: []string{"AmazonEC2"},
				},
			},
			{
				Dimensions: &types.DimensionValues{
					Key:    types.DimensionService,
					Values: []string{"AmazonRDS"},
				},
			},
		},
	})
	want := "SERVICE=AmazonEC2 OR SERVICE=AmazonRDS"
	if got != want {
		t.Fatalf("formatCEFilter() = %q, want %q", got, want)
	}
}

func TestFormatCEFilterTags(t *testing.T) {
	got := formatCEFilter(&types.Expression{
		Tags: &types.TagValues{
			Key:    aws.String("Environment"),
			Values: []string{"prod", "stage"},
		},
	})
	want := "tag:Environment=prod|stage"
	if got != want {
		t.Fatalf("formatCEFilter() = %q, want %q", got, want)
	}
}

func TestFormatCEFilterCostCategories(t *testing.T) {
	got := formatCEFilter(&types.Expression{
		CostCategories: &types.CostCategoryValues{
			Key:    aws.String("Team"),
			Values: []string{"finops"},
		},
	})
	want := "costCategory:Team=finops"
	if got != want {
		t.Fatalf("formatCEFilter() = %q, want %q", got, want)
	}
}

func TestFormatGetSavingsPlansCoverage(t *testing.T) {
	got := formatGetSavingsPlansCoverage(&costexplorer.GetSavingsPlansCoverageInput{
		TimePeriod: &types.DateInterval{
			Start: aws.String("2026-01-01"),
			End:   aws.String("2026-02-01"),
		},
		Granularity: types.GranularityMonthly,
		Filter: &types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionLinkedAccount,
				Values: []string{"111111111111"},
			},
		},
		NextToken: aws.String("token"),
	})
	for _, want := range []string{
		"CostExplorer.GetSavingsPlansCoverage",
		"start=2026-01-01",
		"granularity=MONTHLY",
		"filter=LINKED_ACCOUNT=111111111111",
		"page=next",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatGetSavingsPlansCoverage() = %q, missing %q", got, want)
		}
	}
}

func TestFormatGetAnomalies(t *testing.T) {
	got := formatGetAnomalies(&costexplorer.GetAnomaliesInput{
		DateInterval: &types.AnomalyDateInterval{
			StartDate: aws.String("2026-01-01"),
			EndDate:   aws.String("2026-01-31"),
		},
		NextPageToken: aws.String("token"),
	})
	for _, want := range []string{
		"CostExplorer.GetAnomalies",
		"start=2026-01-01",
		"end=2026-01-31",
		"page=next",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatGetAnomalies() = %q, missing %q", got, want)
		}
	}
}

func TestWrapGetCostAndUsageLogsWhenVerbose(t *testing.T) {
	var buf strings.Builder
	ctx := WithLog(context.Background(), func(line string) {
		buf.WriteString(line)
		buf.WriteByte('\n')
	})

	inner := &fakeGetCostAndUsageClient{}
	wrapped := WrapGetCostAndUsage(inner)
	if _, err := wrapped.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
		Metrics: []string{"NetAmortizedCost"},
	}); err != nil {
		t.Fatalf("GetCostAndUsage: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls)
	}
	got := strings.TrimSpace(buf.String())
	if !strings.Contains(got, "CostExplorer.GetCostAndUsage") {
		t.Fatalf("got %q", got)
	}
}

func TestWrapGetCostAndUsageQuietWithoutLogger(t *testing.T) {
	inner := &fakeGetCostAndUsageClient{}
	wrapped := WrapGetCostAndUsage(inner)
	if _, err := wrapped.GetCostAndUsage(context.Background(), &costexplorer.GetCostAndUsageInput{}); err != nil {
		t.Fatalf("GetCostAndUsage: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1", inner.calls)
	}
}

type fakeGetCostAndUsageClient struct {
	calls int
}

func (f *fakeGetCostAndUsageClient) GetCostAndUsage(
	_ context.Context,
	_ *costexplorer.GetCostAndUsageInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	f.calls++
	return &costexplorer.GetCostAndUsageOutput{}, nil
}
