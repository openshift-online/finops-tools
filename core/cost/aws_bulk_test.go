package cost

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

func TestFetchBulkManyLinkedAccounts(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	ce := &fakeCECaptureFilter{
		fakeCE: fakeCE{
			pages: [][]types.ResultByTime{{
				{
					Groups: []types.Group{
						{
							Keys: []string{"111111111111"},
							Metrics: map[string]types.MetricValue{
								MetricNetAmortized: {Amount: aws.String("10"), Unit: aws.String("USD")},
							},
						},
						{
							Keys: []string{"222222222222"},
							Metrics: map[string]types.MetricValue{
								MetricNetAmortized: {Amount: aws.String("20"), Unit: aws.String("USD")},
							},
						},
						{
							Keys: []string{"999999999999"},
							Metrics: map[string]types.MetricValue{
								MetricNetAmortized: {Amount: aws.String("1000"), Unit: aws.String("USD")},
							},
						},
					},
				},
			}},
		},
	}

	targets := []AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012", ScopeAccountOnly: true, AWSConfig: aws.Config{}, DisplayName: "A"},
		{AccountID: "222222222222", PayerAccountID: "123456789012", ScopeAccountOnly: true, AWSConfig: aws.Config{}, DisplayName: "B"},
	}

	res, err := fetchAWSNetAmortizedBulk(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Range:    LastNDaysRange(30, now),
	}, targets, fetchAWSOptions{
		Now:             now,
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}
	if ce.calls != 1 {
		t.Fatalf("expected 1 Cost Explorer call, got %d", ce.calls)
	}
	if ce.lastFilter == nil || ce.lastFilter.Dimensions == nil {
		t.Fatal("expected linked account filter on Cost Explorer call")
	}
	if got := ce.lastFilter.Dimensions.Values; len(got) != 2 || got[0] != "111111111111" || got[1] != "222222222222" {
		t.Fatalf("filter values = %v, want [111111111111 222222222222]", got)
	}
	if res.Amount != 30 {
		t.Fatalf("Amount = %v, want 30", res.Amount)
	}
}

func TestPlanBulkFetchRequiresSharedPayer(t *testing.T) {
	_, ok := planBulkFetch([]AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012", ScopeAccountOnly: true},
		{AccountID: "222222222222", PayerAccountID: "987654321098", ScopeAccountOnly: true},
	})
	if ok {
		t.Fatal("expected mixed payers to disable bulk fetch")
	}
}

func TestBatchStrings(t *testing.T) {
	batches := batchStrings([]string{"a", "b", "c", "d", "e"}, 2)
	if len(batches) != 3 || len(batches[0]) != 2 || len(batches[2]) != 1 {
		t.Fatalf("batches = %+v", batches)
	}
}

type concurrentCE struct {
	active        int32
	maxConcurrent int32
	calls         int32
}

func (f *concurrentCE) GetCostAndUsage(
	ctx context.Context,
	params *costexplorer.GetCostAndUsageInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	cur := atomic.AddInt32(&f.active, 1)
	for {
		prev := atomic.LoadInt32(&f.maxConcurrent)
		if cur <= prev || atomic.CompareAndSwapInt32(&f.maxConcurrent, prev, cur) {
			break
		}
	}
	atomic.AddInt32(&f.calls, 1)

	select {
	case <-time.After(20 * time.Millisecond):
	case <-ctx.Done():
		atomic.AddInt32(&f.active, -1)
		return nil, ctx.Err()
	}
	atomic.AddInt32(&f.active, -1)

	var groups []types.Group
	if params.Filter != nil && params.Filter.Dimensions != nil {
		for _, accountID := range params.Filter.Dimensions.Values {
			groups = append(groups, types.Group{
				Keys: []string{accountID},
				Metrics: map[string]types.MetricValue{
					MetricNetAmortized: {Amount: aws.String("1"), Unit: aws.String("USD")},
				},
			})
		}
	}
	return &costexplorer.GetCostAndUsageOutput{
		ResultsByTime: []types.ResultByTime{{Groups: groups}},
	}, nil
}

func TestFetchBulkParallelBatches(t *testing.T) {
	const accountCount = 150
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	ce := &concurrentCE{}
	targets := make([]AccountTarget, accountCount)
	for i := 0; i < accountCount; i++ {
		id := fmt.Sprintf("%012d", i+1)
		targets[i] = AccountTarget{
			AccountID:        id,
			PayerAccountID:   "123456789012",
			ScopeAccountOnly: true,
			AWSConfig:        aws.Config{},
		}
	}

	res, err := fetchAWSNetAmortizedBulk(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Range:    LastNDaysRange(30, now),
		SplitBy:  SplitByAccount,
		Workers:  4,
	}, targets, fetchAWSOptions{
		Now:             now,
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}
	if ce.calls != 2 {
		t.Fatalf("expected 2 batched Cost Explorer calls, got %d", ce.calls)
	}
	if ce.maxConcurrent < 2 {
		t.Fatalf("expected parallel batch execution, max concurrent = %d", ce.maxConcurrent)
	}
	if res.Amount != float64(accountCount) {
		t.Fatalf("Amount = %v, want %d", res.Amount, accountCount)
	}
}
