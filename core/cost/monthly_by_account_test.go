package cost

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

// fakeMonthlyByAccountCE returns two months of data, with two (account,
// service) groups in the first month and one in the second, to exercise
// both grouping dimensions plus pagination across months at once.
type fakeMonthlyByAccountCE struct {
	calls int
}

func (f *fakeMonthlyByAccountCE) GetCostAndUsage(
	_ context.Context,
	params *costexplorer.GetCostAndUsageInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	f.calls++
	return &costexplorer.GetCostAndUsageOutput{
		ResultsByTime: []types.ResultByTime{
			{
				TimePeriod: &types.DateInterval{Start: aws.String("2026-01-01"), End: aws.String("2026-02-01")},
				Groups: []types.Group{
					{
						Keys: []string{"111111111111", "Amazon EC2"},
						Metrics: map[string]types.MetricValue{
							MetricNetAmortized: {Amount: aws.String("100.00"), Unit: aws.String("USD")},
						},
					},
					{
						Keys: []string{"222222222222", "Amazon S3"},
						Metrics: map[string]types.MetricValue{
							MetricNetAmortized: {Amount: aws.String("50.00"), Unit: aws.String("USD")},
						},
					},
				},
			},
			{
				TimePeriod: &types.DateInterval{Start: aws.String("2026-02-01"), End: aws.String("2026-03-01")},
				Groups: []types.Group{
					{
						Keys: []string{"111111111111", "Amazon EC2"},
						Metrics: map[string]types.MetricValue{
							MetricNetAmortized: {Amount: aws.String("120.00"), Unit: aws.String("USD")},
						},
					},
				},
			},
		},
	}, nil
}

func TestFetchAccountMonthlyByAccountAndServiceWith(t *testing.T) {
	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	dr := LastNCalendarMonthsRange(2, now)
	fake := &fakeMonthlyByAccountCE{}
	opts := fetchAWSOptions{
		Now: now,
		NewCostExplorer: func(aws.Config) CostExplorerAPI {
			return fake
		},
	}

	rows, err := fetchAccountMonthlyByAccountAndServiceWith(context.Background(), AccountTarget{
		AccountID: "123456789012",
		AWSConfig: aws.Config{Region: CostExplorerRegion},
	}, dr, opts)
	if err != nil {
		t.Fatalf("fetchAccountMonthlyByAccountAndServiceWith() error = %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %+v", len(rows), rows)
	}
	// Sorted by month, then account, then service.
	want := []MonthlyAccountServiceCost{
		{AccountID: "111111111111", Service: "Amazon EC2", Month: "2026-01", Amount: 100, Currency: "USD"},
		{AccountID: "222222222222", Service: "Amazon S3", Month: "2026-01", Amount: 50, Currency: "USD"},
		{AccountID: "111111111111", Service: "Amazon EC2", Month: "2026-02", Amount: 120, Currency: "USD"},
	}
	for i, w := range want {
		if rows[i] != w {
			t.Fatalf("row[%d] = %+v, want %+v", i, rows[i], w)
		}
	}
}

func TestFetchMonthlyByAccountAndServiceWithCombinesMultipleTargets(t *testing.T) {
	fake1 := &fakeMonthlyByAccountCE{}
	fake2 := &fakeMonthlyByAccountCE{}
	got, err := fetchMonthlyByAccountAndServiceWith(context.Background(), CostQuery{
		Accounts: []AccountTarget{
			{AccountID: "123456789012", AWSConfig: aws.Config{Region: "payer-a"}},
			{AccountID: "987654321098", AWSConfig: aws.Config{Region: "payer-b"}},
		},
		Range:   LastNCalendarMonthsRange(2, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
		Workers: 1,
	}, fetchAWSOptions{
		NewCostExplorer: func(cfg aws.Config) CostExplorerAPI {
			if cfg.Region == "payer-a" {
				return fake1
			}
			return fake2
		},
	})
	if err != nil {
		t.Fatalf("fetchMonthlyByAccountAndServiceWith() error = %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("got %d rows, want 6 (3 per payer): %+v", len(got), got)
	}
}

type failingMonthlyByAccountCE struct{}

func (failingMonthlyByAccountCE) GetCostAndUsage(
	context.Context,
	*costexplorer.GetCostAndUsageInput,
	...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	return nil, fmt.Errorf("throttled")
}

func TestFetchMonthlyByAccountAndServiceWithAbortsBatchOnAnyFailure(t *testing.T) {
	_, err := fetchMonthlyByAccountAndServiceWith(context.Background(), CostQuery{
		Accounts: []AccountTarget{
			{AccountID: "123456789012", AWSConfig: aws.Config{Region: "ok"}},
			{AccountID: "987654321098", AWSConfig: aws.Config{Region: "fail"}},
		},
		Range:   LastNCalendarMonthsRange(1, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
		Workers: 1,
	}, fetchAWSOptions{
		NewCostExplorer: func(cfg aws.Config) CostExplorerAPI {
			if cfg.Region == "fail" {
				return failingMonthlyByAccountCE{}
			}
			return &fakeMonthlyByAccountCE{}
		},
	})
	if err == nil {
		t.Fatal("expected an error when one target's Cost Explorer query fails")
	}
}
