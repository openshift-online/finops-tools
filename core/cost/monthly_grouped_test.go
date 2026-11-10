// monthly_grouped_test.go tests the monthly LINKED_ACCOUNT+SERVICE grouped Cost Explorer fetch.
package cost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

func groupedRow(month string, accountID, service, amount, unit string) types.ResultByTime {
	return types.ResultByTime{
		TimePeriod: &types.DateInterval{Start: aws.String(month + "-01"), End: aws.String(month + "-31")},
		Groups: []types.Group{
			{
				Keys: []string{accountID, service},
				Metrics: map[string]types.MetricValue{
					MetricNetAmortized: {Amount: aws.String(amount), Unit: aws.String(unit)},
				},
			},
		},
	}
}

func totalRow(month string, amount, unit string) types.ResultByTime {
	return types.ResultByTime{
		TimePeriod: &types.DateInterval{Start: aws.String(month + "-01"), End: aws.String(month + "-31")},
		Total: map[string]types.MetricValue{
			MetricNetAmortized: {Amount: aws.String(amount), Unit: aws.String(unit)},
		},
	}
}

func TestSumNetAmortizedMonthlyGroupedRequestShapeAndParsing(t *testing.T) {
	captured := &capturedGetCostAndUsageInput{fakeCE: fakeCE{pages: [][]types.ResultByTime{
		{
			groupedRow("2026-06", "111111111111", "Amazon EC2", "10.5", "USD"),
			groupedRow("2026-07", "111111111111", "Amazon EC2", "20", "USD"),
		},
		{
			groupedRow("2026-07", "222222222222", "AWS Lambda", "0.25", "USD"),
		},
	}}}

	dr := DateRange{
		Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	rows, currency, err := sumNetAmortizedMonthlyGrouped(context.Background(), captured, dr, nil)
	if err != nil {
		t.Fatal(err)
	}

	if currency != "USD" {
		t.Errorf("currency = %q, want USD", currency)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	want := map[string]float64{
		"2026-06|111111111111|Amazon EC2": 10.5,
		"2026-07|111111111111|Amazon EC2": 20,
		"2026-07|222222222222|AWS Lambda": 0.25,
	}
	for _, r := range rows {
		key := r.Month + "|" + r.AccountID + "|" + r.Service
		got, ok := want[key]
		if !ok {
			t.Fatalf("unexpected row %q", key)
		}
		if r.Amount != got {
			t.Errorf("row %q amount = %v, want %v", key, r.Amount, got)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("missing rows: %v", want)
	}
	if captured.calls != 2 {
		t.Errorf("calls = %d, want 2 (pagination consumed)", captured.calls)
	}

	in := captured.inputs[0]
	if in.Granularity != types.GranularityMonthly {
		t.Errorf("Granularity = %v, want MONTHLY", in.Granularity)
	}
	if len(in.Metrics) != 1 || in.Metrics[0] != MetricNetAmortized {
		t.Errorf("Metrics = %v, want [NetAmortizedCost]", in.Metrics)
	}
	if len(in.GroupBy) != 2 ||
		in.GroupBy[0].Key == nil || *in.GroupBy[0].Key != "LINKED_ACCOUNT" ||
		in.GroupBy[1].Key == nil || *in.GroupBy[1].Key != "SERVICE" {
		t.Errorf("GroupBy = %+v, want [LINKED_ACCOUNT, SERVICE]", in.GroupBy)
	}
	if in.GroupBy[0].Type != types.GroupDefinitionTypeDimension || in.GroupBy[1].Type != types.GroupDefinitionTypeDimension {
		t.Errorf("GroupBy types = [%v, %v], want DIMENSION for both", in.GroupBy[0].Type, in.GroupBy[1].Type)
	}
	if aws.ToString(in.TimePeriod.Start) != "2026-06-01" || aws.ToString(in.TimePeriod.End) != "2026-08-01" {
		t.Errorf("TimePeriod = %s..%s, want 2026-06-01..2026-08-01 (half-open)",
			aws.ToString(in.TimePeriod.Start), aws.ToString(in.TimePeriod.End))
	}
}

func TestSumNetAmortizedMonthlyGroupedMixedCurrencyFails(t *testing.T) {
	ce := &fakeCE{pages: [][]types.ResultByTime{
		{
			groupedRow("2026-06", "111111111111", "Amazon EC2", "10", "USD"),
			groupedRow("2026-06", "222222222222", "Amazon EC2", "5", "EUR"),
		},
	}}
	dr := DateRange{
		Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
	_, _, err := sumNetAmortizedMonthlyGrouped(context.Background(), ce, dr, nil)
	if err == nil || !strings.Contains(err.Error(), "currency") {
		t.Fatalf("err = %v, want mixed currency error", err)
	}
}

func TestSumNetAmortizedMonthlyGroupedBadAmountFails(t *testing.T) {
	ce := &fakeCE{pages: [][]types.ResultByTime{
		{groupedRow("2026-06", "111111111111", "Amazon EC2", "not-a-number", "USD")},
	}}
	dr := DateRange{
		Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
	_, _, err := sumNetAmortizedMonthlyGrouped(context.Background(), ce, dr, nil)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("err = %v, want amount parse error", err)
	}
}

func TestSumNetAmortizedMonthlyGroupedCEErrorFails(t *testing.T) {
	ce := failingMonthlyCE{}
	dr := DateRange{
		Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
	if _, _, err := sumNetAmortizedMonthlyGrouped(context.Background(), ce, dr, nil); err == nil {
		t.Fatal("expected error")
	}
}

// capturedGetCostAndUsageInput records every GetCostAndUsageInput like fakeCE but
// keeps the params for shape assertions.
type capturedGetCostAndUsageInput struct {
	fakeCE
	inputs []*costexplorer.GetCostAndUsageInput
}

func (c *capturedGetCostAndUsageInput) GetCostAndUsage(
	_ context.Context,
	params *costexplorer.GetCostAndUsageInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	c.inputs = append(c.inputs, params)
	return c.fakeCE.GetCostAndUsage(context.Background(), params)
}
