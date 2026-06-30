package snapshot

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

type fakeCostExplorer struct {
	pages     [][]types.Group
	page      int
	callCount int
}

func (f *fakeCostExplorer) GetCostAndUsage(
	_ context.Context,
	_ *costexplorer.GetCostAndUsageInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	f.callCount++
	if f.page >= len(f.pages) {
		return &costexplorer.GetCostAndUsageOutput{}, nil
	}
	out := &costexplorer.GetCostAndUsageOutput{
		ResultsByTime: []types.ResultByTime{{
			Groups: f.pages[f.page],
		}},
	}
	f.page++
	if f.page < len(f.pages) {
		out.NextPageToken = aws.String("next")
	}
	return out, nil
}

func TestLastCompleteMonthRange(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	start, end := LastCompleteMonthRange(now)
	if start.Format("2006-01-02") != "2026-05-01" {
		t.Fatalf("start = %s", start)
	}
	if end.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("end = %s", end)
	}
}

func TestFetchBilledSnapshotCosts(t *testing.T) {
	ce := &fakeCostExplorer{
		pages: [][]types.Group{
			{{
				Keys: []string{"EBS:SnapshotUsage"},
				Metrics: map[string]types.MetricValue{
					"UnblendedCost":  {Amount: aws.String("26.66"), Unit: aws.String("USD")},
					"UsageQuantity":  {Amount: aws.String("701.51"), Unit: aws.String("GB-Month")},
				},
			}},
			{{
				Keys: []string{"RDS:ChargedBackupUsage"},
				Metrics: map[string]types.MetricValue{
					"UnblendedCost": {Amount: aws.String("3189.99"), Unit: aws.String("USD")},
					"UsageQuantity": {Amount: aws.String("44182"), Unit: aws.String("GB-Month")},
				},
			}},
		},
	}
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	got, err := FetchBilledSnapshotCosts(context.Background(), ce, []string{"111111111111"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d", len(got))
	}
	if got[0].EBSSnapshotUSD != 26.66 {
		t.Fatalf("ebs usd = %v", got[0].EBSSnapshotUSD)
	}
	if got[0].RDSBackupUSD != 3189.99 {
		t.Fatalf("rds usd = %v", got[0].RDSBackupUSD)
	}
	if got[0].Period.StartDate != "2026-05-01" || got[0].Period.EndDate != "2026-05-31" {
		t.Fatalf("period = %#v", got[0].Period)
	}
}

func TestFetchBilledSnapshotCostsDeduplicatesAccountIDs(t *testing.T) {
	ce := &fakeCostExplorer{
		pages: [][]types.Group{{
			{
				Keys: []string{"EBS:SnapshotUsage"},
				Metrics: map[string]types.MetricValue{
					"UnblendedCost": {Amount: aws.String("10.00"), Unit: aws.String("USD")},
				},
			},
		}},
	}
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	got, err := FetchBilledSnapshotCosts(
		context.Background(),
		ce,
		[]string{"111111111111", " 111111111111 ", "111111111111"},
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	if got[0].AccountID != "111111111111" {
		t.Fatalf("account_id = %q", got[0].AccountID)
	}
	if ce.callCount != 1 {
		t.Fatalf("cost explorer calls = %d, want 1", ce.callCount)
	}
}
