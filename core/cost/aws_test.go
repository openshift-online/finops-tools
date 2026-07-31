// aws_test.go tests AWS Cost Explorer fetch, grouping, and response parsing.
package cost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

type fakeCE struct {
	pages [][]types.ResultByTime
	calls int
}

func (f *fakeCE) GetCostAndUsage(
	_ context.Context,
	params *costexplorer.GetCostAndUsageInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	f.calls++
	idx := f.calls - 1
	if idx >= len(f.pages) {
		return &costexplorer.GetCostAndUsageOutput{}, nil
	}
	var token *string
	if idx+1 < len(f.pages) {
		tok := "next"
		token = &tok
	}
	return &costexplorer.GetCostAndUsageOutput{
		ResultsByTime: f.pages[idx],
		NextPageToken: token,
	}, nil
}

func TestSumNetAmortizedCost(t *testing.T) {
	ce := &fakeCE{
		pages: [][]types.ResultByTime{
			{
				{Total: map[string]types.MetricValue{
					MetricNetAmortized: {Amount: aws.String("10.5"), Unit: aws.String("USD")},
				}},
			},
			{
				{Total: map[string]types.MetricValue{
					MetricNetAmortized: {Amount: aws.String("2.25"), Unit: aws.String("USD")},
				}},
			},
		},
	}

	dr := DateRange{
		Start: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
	}
	total, currency, err := sumNetAmortizedCost(context.Background(), ce, dr, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 12.75 {
		t.Errorf("total = %v, want 12.75", total)
	}
	if currency != "USD" {
		t.Errorf("currency = %q, want USD", currency)
	}
	if ce.calls != 2 {
		t.Errorf("calls = %d, want 2", ce.calls)
	}
}

func TestFetchAWSNetAmortizedWith(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	ce := &fakeCE{
		pages: [][]types.ResultByTime{{
			{Total: map[string]types.MetricValue{
				MetricNetAmortized: {Amount: aws.String("100"), Unit: aws.String("USD")},
			}},
		}},
	}

	res, err := fetchAWSNetAmortizedWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: []AccountTarget{{
			AccountID:   "123456789012",
			AWSConfig:   aws.Config{},
			DisplayName: "rh-control",
		}},
		Range: LastNDaysRange(30, now),
	}, fetchAWSOptions{
		Now:             now,
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 100 {
		t.Errorf("Amount = %v, want 100", res.Amount)
	}
	if res.AccountID != "123456789012" {
		t.Errorf("AccountID = %q", res.AccountID)
	}
	if res.AccountName != "rh-control" {
		t.Errorf("AccountName = %q", res.AccountName)
	}
	if res.StartDate != "2026-04-25" {
		t.Errorf("StartDate = %q", res.StartDate)
	}
	if res.EndDate != "2026-05-24" {
		t.Errorf("EndDate = %q, want inclusive last day 2026-05-24", res.EndDate)
	}
}

func TestFetchAWSNetAmortizedLinkedAccount(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	ce := &fakeCE{
		pages: [][]types.ResultByTime{{
			{Total: map[string]types.MetricValue{
				MetricNetAmortized: {Amount: aws.String("42"), Unit: aws.String("USD")},
			}},
		}},
	}

	res, err := fetchAWSNetAmortizedWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: []AccountTarget{{
			AccountID:      "111111111111",
			PayerAccountID: "123456789012",
			AWSConfig:      aws.Config{},
			DisplayName:    "Quay Production",
		}},
		Range: LastNDaysRange(30, now),
	}, fetchAWSOptions{
		Now:             now,
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 42 {
		t.Errorf("Amount = %v, want 42", res.Amount)
	}
	if res.AccountID != "111111111111" {
		t.Errorf("AccountID = %q", res.AccountID)
	}
	if res.AccountName != "Quay Production" {
		t.Errorf("AccountName = %q, want Quay Production", res.AccountName)
	}
	if !res.Linked {
		t.Error("expected Linked=true")
	}
}

func TestAccountDisplayNameFallsBackToDisplayAlias(t *testing.T) {
	name := AccountTarget{
		AccountID:    "111111111111",
		DisplayAlias: "quay",
	}.AccountDisplayName()
	if name != "quay" {
		t.Errorf("name = %q, want quay", name)
	}
}

func TestFetchEmptyAccount(t *testing.T) {
	_, err := Fetch(context.Background(), CostQuery{Provider: ProviderAWS})
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeCECaptureFilter struct {
	fakeCE
	lastFilter *types.Expression
}

func (f *fakeCECaptureFilter) GetCostAndUsage(
	ctx context.Context,
	params *costexplorer.GetCostAndUsageInput,
	optFns ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	f.lastFilter = params.Filter
	return f.fakeCE.GetCostAndUsage(ctx, params, optFns...)
}

func TestFetchAWSNetAmortizedScopeAccountOnly(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	ce := &fakeCECaptureFilter{
		fakeCE: fakeCE{
			pages: [][]types.ResultByTime{{
				{Total: map[string]types.MetricValue{
					MetricNetAmortized: {Amount: aws.String("100"), Unit: aws.String("USD")},
				}},
			}},
		},
	}

	_, err := fetchAWSNetAmortizedWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: []AccountTarget{{
			AccountID:        "123456789012",
			PayerAccountID:   "123456789012",
			ScopeAccountOnly: true,
			AWSConfig:        aws.Config{},
		}},
		Range: LastNDaysRange(30, now),
	}, fetchAWSOptions{
		Now:             now,
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}
	if ce.lastFilter == nil || ce.lastFilter.Dimensions == nil {
		t.Fatal("expected linked account filter")
	}
	if ce.lastFilter.Dimensions.Key != types.DimensionLinkedAccount {
		t.Fatalf("filter key = %q", ce.lastFilter.Dimensions.Key)
	}
	if len(ce.lastFilter.Dimensions.Values) != 1 || ce.lastFilter.Dimensions.Values[0] != "123456789012" {
		t.Fatalf("filter values = %v", ce.lastFilter.Dimensions.Values)
	}
}

func TestFetchGCPNotImplemented(t *testing.T) {
	_, err := Fetch(context.Background(), CostQuery{
		Provider: ProviderGCP,
		Accounts: []AccountTarget{{AccountID: "123456789012", AWSConfig: aws.Config{}}},
	})
	if err == nil || !errors.Is(err, errProviderNotImplemented) {
		t.Fatalf("expected not implemented, got %v", err)
	}
}

func TestSumNetAmortizedByService(t *testing.T) {
	ce := &fakeCE{
		pages: [][]types.ResultByTime{{
			{
				Groups: []types.Group{
					{
						Keys: []string{"Amazon EC2"},
						Metrics: map[string]types.MetricValue{
							MetricNetAmortized: {Amount: aws.String("40"), Unit: aws.String("USD")},
						},
					},
					{
						Keys: []string{"Amazon S3"},
						Metrics: map[string]types.MetricValue{
							MetricNetAmortized: {Amount: aws.String("10"), Unit: aws.String("USD")},
						},
					},
				},
			},
			{
				Groups: []types.Group{
					{
						Keys: []string{"Amazon EC2"},
						Metrics: map[string]types.MetricValue{
							MetricNetAmortized: {Amount: aws.String("5"), Unit: aws.String("USD")},
						},
					},
				},
			},
		}},
	}

	dr := DateRange{
		Start: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC),
	}
	total, currency, breakdown, err := sumNetAmortizedGrouped(context.Background(), ce, dr, "SERVICE", GroupByService, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 55 {
		t.Errorf("total = %v, want 55", total)
	}
	if currency != "USD" {
		t.Errorf("currency = %q", currency)
	}
	if len(breakdown) != 2 {
		t.Fatalf("breakdown len = %d", len(breakdown))
	}
	if breakdown[0].Service != "Amazon EC2" || breakdown[0].Amount != 45 {
		t.Errorf("first = %+v", breakdown[0])
	}
}

func TestSumNetAmortizedByAccount(t *testing.T) {
	ce := &fakeCE{
		pages: [][]types.ResultByTime{{
			{Groups: []types.Group{
				{
					Keys: []string{"111111111111"},
					Metrics: map[string]types.MetricValue{
						MetricNetAmortized: {Amount: aws.String("70"), Unit: aws.String("USD")},
					},
				},
				{
					Keys: []string{"222222222222"},
					Metrics: map[string]types.MetricValue{
						MetricNetAmortized: {Amount: aws.String("30"), Unit: aws.String("USD")},
					},
				},
			}},
		}},
	}
	dr := DateRange{
		Start: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
	}
	total, _, breakdown, err := sumNetAmortizedGrouped(context.Background(), ce, dr, "LINKED_ACCOUNT", GroupByAccount, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 100 || len(breakdown) != 2 {
		t.Fatalf("total=%v breakdown=%+v", total, breakdown)
	}
	if breakdown[0].Account != "111111111111" || breakdown[0].Amount != 70 {
		t.Errorf("first = %+v", breakdown[0])
	}
}

func TestFetchAWSNetAmortizedByService(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	ce := &fakeCE{
		pages: [][]types.ResultByTime{{
			{Groups: []types.Group{{
				Keys:    []string{"Amazon S3"},
				Metrics: map[string]types.MetricValue{
					MetricNetAmortized: {Amount: aws.String("25"), Unit: aws.String("USD")},
				},
			}}},
		}},
	}

	res, err := fetchAWSNetAmortizedWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: []AccountTarget{{AccountID: "123456789012", AWSConfig: aws.Config{}}},
		Range:   LastNDaysRange(30, now),
		GroupBy: GroupByService,
	}, fetchAWSOptions{
		Now:             now,
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.GroupBy != GroupByService {
		t.Errorf("GroupBy = %q", res.GroupBy)
	}
	if len(res.Breakdown) != 1 || res.Breakdown[0].Amount != 25 {
		t.Errorf("Breakdown = %+v", res.Breakdown)
	}
}

func TestApplyAWSAccountNamesResolve(t *testing.T) {
	breakdown := []CostBreakdownItem{
		{Account: "111111111111", Amount: 50},
		{Account: "222222222222", Amount: 30},
	}
	resolve := func(_ context.Context, _ aws.Config, ids []string) (map[string]string, error) {
		if len(ids) != 2 {
			t.Fatalf("ids = %v", ids)
		}
		return map[string]string{
			"111111111111": "Member One",
			"222222222222": "Member Two",
		}, nil
	}
	out := applyAWSAccountNames(context.Background(), aws.Config{}, breakdown, fetchAWSOptions{
		ResolveAccountNames: resolve,
	})
	if out[0].AccountName != "Member One" || out[1].AccountName != "Member Two" {
		t.Fatalf("breakdown = %+v", out)
	}
}

func TestSumNetAmortizedDaily(t *testing.T) {
	ce := &fakeCE{
		pages: [][]types.ResultByTime{
			{
				{
					TimePeriod: &types.DateInterval{Start: aws.String("2026-04-25"), End: aws.String("2026-04-26")},
					Total: map[string]types.MetricValue{
						MetricNetAmortized: {Amount: aws.String("10"), Unit: aws.String("USD")},
					},
				},
				{
					TimePeriod: &types.DateInterval{Start: aws.String("2026-04-26"), End: aws.String("2026-04-27")},
					Total: map[string]types.MetricValue{
						MetricNetAmortized: {Amount: aws.String("5"), Unit: aws.String("USD")},
					},
				},
			},
			{
				{
					TimePeriod: &types.DateInterval{Start: aws.String("2026-04-27"), End: aws.String("2026-04-28")},
					Total: map[string]types.MetricValue{
						MetricNetAmortized: {Amount: aws.String("2.5"), Unit: aws.String("USD")},
					},
				},
			},
		},
	}

	dr := DateRange{
		Start: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC),
	}
	daily, currency, err := sumNetAmortizedDaily(context.Background(), ce, dr, nil)
	if err != nil {
		t.Fatal(err)
	}
	if currency != "USD" {
		t.Errorf("currency = %q", currency)
	}
	if len(daily) != 3 {
		t.Fatalf("daily len = %d, want 3: %+v", len(daily), daily)
	}
	if daily[0].Date != "2026-04-25" || daily[0].Amount != 10 {
		t.Errorf("first = %+v", daily[0])
	}
	if daily[2].Date != "2026-04-27" || daily[2].Amount != 2.5 {
		t.Errorf("last = %+v", daily[2])
	}
}

func TestFetchAWSDailyNetAmortizedWith(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	ce := &fakeCE{
		pages: [][]types.ResultByTime{{
			{
				TimePeriod: &types.DateInterval{Start: aws.String("2026-05-24"), End: aws.String("2026-05-25")},
				Total: map[string]types.MetricValue{
					MetricNetAmortized: {Amount: aws.String("99"), Unit: aws.String("USD")},
				},
			},
		}},
	}

	daily, currency, err := fetchAWSDailyNetAmortizedWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: []AccountTarget{{AccountID: "123456789012", AWSConfig: aws.Config{}}},
		Range: LastNDaysRange(30, now),
	}, fetchAWSOptions{
		Now:             now,
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}
	if currency != "USD" {
		t.Errorf("currency = %q", currency)
	}
	if len(daily) != 1 || daily[0].Amount != 99 {
		t.Errorf("daily = %+v", daily)
	}
}

func TestParseProvider(t *testing.T) {
	p, err := ParseProvider("AWS")
	if err != nil || p != ProviderAWS {
		t.Fatalf("got %v %v", p, err)
	}
	_, err = ParseProvider("azure")
	if err == nil {
		t.Fatal("expected error")
	}
}
