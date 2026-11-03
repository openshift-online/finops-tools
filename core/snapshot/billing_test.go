package snapshot

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

type fakeCostExplorer struct {
	mu        sync.Mutex
	pages     [][]types.Group
	page      int
	callCount int
}

func (f *fakeCostExplorer) GetCostAndUsage(
	_ context.Context,
	_ *costexplorer.GetCostAndUsageInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
				Keys: []string{"111111111111", "EBS:SnapshotUsage"},
				Metrics: map[string]types.MetricValue{
					"UnblendedCost": {Amount: aws.String("26.66"), Unit: aws.String("USD")},
					"UsageQuantity": {Amount: aws.String("701.51"), Unit: aws.String("GB-Month")},
				},
			}},
			{{
				Keys: []string{"111111111111", "RDS:ChargedBackupUsage"},
				Metrics: map[string]types.MetricValue{
					"UnblendedCost": {Amount: aws.String("3189.99"), Unit: aws.String("USD")},
					"UsageQuantity": {Amount: aws.String("44182"), Unit: aws.String("GB-Month")},
				},
			}},
		},
	}
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	got, err := FetchBilledSnapshotCosts(context.Background(), ce, []string{"111111111111"}, now, 1)
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
				Keys: []string{"111111111111", "EBS:SnapshotUsage"},
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
		1,
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

func TestFetchBilledSnapshotCostsBatchesAccounts(t *testing.T) {
	ce := &fakeCostExplorer{
		pages: [][]types.Group{{
			{
				Keys: []string{"111111111111", "EBS:SnapshotUsage"},
				Metrics: map[string]types.MetricValue{
					"UnblendedCost": {Amount: aws.String("1.00"), Unit: aws.String("USD")},
				},
			},
		}},
	}
	now := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)

	ids := make([]string, 0, 150)
	for i := 0; i < 150; i++ {
		ids = append(ids, fmt.Sprintf("%012d", 111111111111+i))
	}
	got, err := FetchBilledSnapshotCosts(context.Background(), ce, ids, now, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 150 {
		t.Fatalf("rows = %d, want 150", len(got))
	}
	if ce.callCount != 2 {
		t.Fatalf("cost explorer calls = %d, want 2", ce.callCount)
	}
}

type costExplorerFunc func(*costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error)

func (f costExplorerFunc) GetCostAndUsage(_ context.Context, input *costexplorer.GetCostAndUsageInput, _ ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	return f(input)
}

func TestRegionalBillingGroupsAccountScopesAndPreservesPagination(t *testing.T) {
	scopes := []AccountScanScope{
		{AccountID: "111111111111", Regions: []string{" us-west-2 ", "us-east-1", "us-west-2"}},
		{AccountID: "222222222222", Regions: []string{"us-east-1", "us-west-2"}},
		{AccountID: "333333333333", Regions: []string{"eu-west-1"}},
		{AccountID: "444444444444", Regions: []string{}},
	}
	requests := 0
	ce := costExplorerFunc(func(input *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
		requests++
		if len(input.Filter.And) != 2 {
			t.Fatalf("filter = %+v", input.Filter)
		}
		accounts := input.Filter.And[0].Dimensions
		regions := input.Filter.And[1].Dimensions
		if accounts.Key != types.DimensionLinkedAccount || regions.Key != types.DimensionRegion {
			t.Fatalf("accounts=%+v regions=%+v", accounts, regions)
		}
		if reflect.DeepEqual(regions.Values, []string{"us-east-1", "us-west-2"}) {
			if !reflect.DeepEqual(accounts.Values, []string{"111111111111", "222222222222"}) {
				t.Fatalf("shared region scope accounts = %v", accounts.Values)
			}
		} else if !reflect.DeepEqual(regions.Values, []string{"eu-west-1"}) || !reflect.DeepEqual(accounts.Values, []string{"333333333333"}) {
			t.Fatalf("scope leaked across accounts: accounts=%v regions=%v", accounts.Values, regions.Values)
		}
		if len(input.GroupBy) != 2 || aws.ToString(input.GroupBy[0].Key) != "LINKED_ACCOUNT" || aws.ToString(input.GroupBy[1].Key) != "USAGE_TYPE" {
			t.Fatalf("grouping = %v", input.GroupBy)
		}
		if aws.ToString(input.TimePeriod.Start) != "2026-05-01" || aws.ToString(input.TimePeriod.End) != "2026-06-01" {
			t.Fatalf("period = %v", input.TimePeriod)
		}
		usageType, amount := "EBS:SnapshotUsage", "100"
		var next *string
		if aws.ToString(input.NextPageToken) == "" {
			next = aws.String("rds-page")
		} else {
			if aws.ToString(input.NextPageToken) != "rds-page" {
				t.Fatalf("pagination token = %v", input.NextPageToken)
			}
			usageType, amount = "RDS:ChargedBackupUsage", "20"
		}
		var groups []types.Group
		for _, id := range accounts.Values {
			groups = append(groups, types.Group{Keys: []string{id, usageType}, Metrics: map[string]types.MetricValue{
				"UnblendedCost": {Amount: aws.String(amount)},
				"UsageQuantity": {Amount: aws.String("50")},
			}})
		}
		return &costexplorer.GetCostAndUsageOutput{NextPageToken: next, ResultsByTime: []types.ResultByTime{{Groups: groups}}}, nil
	})
	got, err := FetchRegionalBilledSnapshotCosts(context.Background(), ce, scopes, time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC), 1)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 4 || len(got) != 4 {
		t.Fatalf("requests=%d costs=%+v", requests, got)
	}
	for _, costs := range got {
		if costs.AccountID == "444444444444" {
			if costs.EBSSnapshotUSD != 0 || costs.RDSBackupUSD != 0 {
				t.Fatalf("empty scope billed costs = %+v", costs)
			}
			continue
		}
		if costs.EBSSnapshotUSD != 100 || costs.RDSBackupUSD != 20 || costs.EBSSnapshotGiBMonth != 50 || costs.RDSBackupGiBMonth != 50 {
			t.Fatalf("regional billed costs = %+v", costs)
		}
	}
}

func TestRegionalBillingEmptyScopeNeverQueriesAccountWide(t *testing.T) {
	ce := costExplorerFunc(func(*costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
		t.Fatal("empty regional scope must not issue any billing request")
		return nil, nil
	})
	for _, regions := range [][]string{nil, {}, {" ", ""}} {
		got, err := FetchRegionalBilledSnapshotCosts(context.Background(), ce, []AccountScanScope{{AccountID: "111111111111", Regions: regions}}, time.Now(), 1)
		if err != nil || len(got) != 1 || got[0].EBSSnapshotUSD != 0 || got[0].RDSBackupUSD != 0 {
			t.Fatalf("regions=%v costs=%v err=%v", regions, got, err)
		}
	}
}

func TestRegionalBillingPreservesFilterAcrossAccountBatches(t *testing.T) {
	var scopes []AccountScanScope
	for i := 0; i < 101; i++ {
		scopes = append(scopes, AccountScanScope{AccountID: fmt.Sprintf("%012d", 111111111111+i), Regions: []string{"us-east-1"}})
	}
	requests, accounts := 0, 0
	ce := costExplorerFunc(func(input *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
		requests++
		accounts += len(input.Filter.And[0].Dimensions.Values)
		if !reflect.DeepEqual(input.Filter.And[1].Dimensions.Values, []string{"us-east-1"}) {
			t.Fatalf("region filter lost: %+v", input.Filter)
		}
		return &costexplorer.GetCostAndUsageOutput{}, nil
	})
	got, err := FetchRegionalBilledSnapshotCosts(context.Background(), ce, scopes, time.Now(), 1)
	if err != nil || len(got) != 101 || accounts != 101 || requests != 2 {
		t.Fatalf("costs=%d accounts=%d requests=%d err=%v", len(got), accounts, requests, err)
	}
}
