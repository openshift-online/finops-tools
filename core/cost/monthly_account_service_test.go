// monthly_account_service_test.go tests the payer-aware monthly account/service collection.
package cost

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

const (
	testPayerID = "123456789012"
	testMemberA = "111111111111"
	testMemberB = "222222222222"
)

// routingCE routes GetCostAndUsage calls by filter shape so tests can distinguish
// member-batch, payer-only, and totals queries.
type routingCE struct {
	pages       [][]types.ResultByTime
	requests    []*costexplorer.GetCostAndUsageInput
	invocations int
}

func (r *routingCE) GetCostAndUsage(
	_ context.Context,
	params *costexplorer.GetCostAndUsageInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	r.requests = append(r.requests, params)
	idx := r.invocations
	r.invocations++
	if idx >= len(r.pages) {
		return &costexplorer.GetCostAndUsageOutput{}, nil
	}
	return &costexplorer.GetCostAndUsageOutput{ResultsByTime: r.pages[idx]}, nil
}

// filterAccountValues returns the LINKED_ACCOUNT dimension filter values, or nil.
func filterAccountValues(in *costexplorer.GetCostAndUsageInput) []string {
	if in.Filter == nil || in.Filter.Dimensions == nil {
		return nil
	}
	return in.Filter.Dimensions.Values
}

func payerMemberTargets() []AccountTarget {
	return []AccountTarget{
		{AccountID: testPayerID, AWSConfig: aws.Config{}, DisplayName: "Payer"},
		{AccountID: testMemberA, PayerAccountID: testPayerID, AWSConfig: aws.Config{}, DisplayName: "Member A"},
		{AccountID: testMemberB, PayerAccountID: testPayerID, AWSConfig: aws.Config{}, DisplayName: "Member B"},
	}
}

func windowRange() DateRange {
	return DateRange{
		Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestFetchMonthlyAccountServicePayerAndMembersReconciled(t *testing.T) {
	ce := &routingCE{pages: [][]types.ResultByTime{
		// member batch (both members, one batch)
		{
			groupedRow("2026-06", testMemberA, "Amazon EC2", "100", "USD"),
			groupedRow("2026-06", testMemberB, "AWS Lambda", "50", "USD"),
		},
		// payer-only grouped query
		{
			groupedRow("2026-06", testPayerID, "Amazon S3", "25", "USD"),
		},
		// totals for selected accounts
		{
			totalRow("2026-06", "175", "USD"),
		},
	}}

	res, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: payerMemberTargets(),
		Range:    windowRange(),
	}, fetchAWSOptions{
		Now:             time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.Currency != "USD" {
		t.Errorf("currency = %q, want USD", res.Currency)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(res.Rows), res.Rows)
	}
	if res.PayerAccountID != testPayerID {
		t.Errorf("PayerAccountID = %q, want %q", res.PayerAccountID, testPayerID)
	}

	// Request shapes: three queries, all month-aligned half-open.
	if len(ce.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(ce.requests))
	}
	for i, in := range ce.requests {
		if in.Granularity != types.GranularityMonthly {
			t.Errorf("request %d granularity = %v, want MONTHLY", i, in.Granularity)
		}
		if aws.ToString(in.TimePeriod.Start) != "2026-06-01" || aws.ToString(in.TimePeriod.End) != "2026-07-01" {
			t.Errorf("request %d TimePeriod = %s..%s", i, aws.ToString(in.TimePeriod.Start), aws.ToString(in.TimePeriod.End))
		}
	}
	// Member batch excludes the payer and filters to the member IDs.
	memberBatch := filterAccountValues(ce.requests[0])
	if len(memberBatch) != 2 || memberBatch[0] != testMemberA || memberBatch[1] != testMemberB {
		t.Errorf("member batch filter = %v, want [%s %s]", memberBatch, testMemberA, testMemberB)
	}
	// Payer query filters to the payer ID only.
	payerFilter := filterAccountValues(ce.requests[1])
	if len(payerFilter) != 1 || payerFilter[0] != testPayerID {
		t.Errorf("payer filter = %v, want [%s]", payerFilter, testPayerID)
	}
	// Totals query covers all selected accounts.
	totalsFilter := filterAccountValues(ce.requests[2])
	if len(totalsFilter) != 3 {
		t.Errorf("totals filter = %v, want 3 accounts", totalsFilter)
	}
	if len(ce.requests[2].GroupBy) != 0 {
		t.Errorf("totals query should not group, got %+v", ce.requests[2].GroupBy)
	}
}

func TestFetchMonthlyAccountServicePayerOnly(t *testing.T) {
	ce := &routingCE{pages: [][]types.ResultByTime{
		// payer grouped query
		{groupedRow("2026-06", testPayerID, "Amazon S3", "25", "USD")},
		// totals
		{totalRow("2026-06", "25", "USD")},
	}}

	res, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: []AccountTarget{{AccountID: testPayerID, AWSConfig: aws.Config{}, DisplayName: "Payer"}},
		Range:    windowRange(),
	}, fetchAWSOptions{
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0].AccountID != testPayerID {
		t.Fatalf("rows = %+v, want single payer row", res.Rows)
	}
}

func TestFetchMonthlyAccountServiceBatchSizeCapped(t *testing.T) {
	targets := []AccountTarget{{AccountID: testPayerID, AWSConfig: aws.Config{}}}
	for i := range 250 {
		// Distinct zero-padded 12-digit IDs.
		id := fmt.Sprintf("%012d", i+1)
		targets = append(targets, AccountTarget{AccountID: id, PayerAccountID: testPayerID, AWSConfig: aws.Config{}})
	}

	ce := &routingCE{}
	// Empty CE responses: totals are 0 and rows are 0, so reconciliation passes.
	if _, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: targets,
		Range:    windowRange(),
	}, fetchAWSOptions{
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	}); err != nil {
		t.Fatal(err)
	}
	// 3 member batches (250 IDs) + 1 payer + 3 totals batches (251 IDs) = 7 requests.
	if len(ce.requests) != 7 {
		t.Fatalf("requests = %d, want 7", len(ce.requests))
	}
	// Every request must respect the linked-account filter batch limit.
	for i, in := range ce.requests {
		if got := len(filterAccountValues(in)); got > linkedAccountFilterBatchSize {
			t.Errorf("request %d filters %d accounts, want <= %d", i, got, linkedAccountFilterBatchSize)
		}
	}
}

func TestFetchMonthlyAccountServiceReconciliationFailure(t *testing.T) {
	ce := &routingCE{pages: [][]types.ResultByTime{
		// member batch
		{groupedRow("2026-06", testMemberA, "Amazon EC2", "100", "USD")},
		// payer
		{groupedRow("2026-06", testPayerID, "Amazon S3", "25", "USD")},
		// totals: off by a dollar
		{totalRow("2026-06", "126", "USD")},
	}}
	_, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: payerMemberTargets(),
		Range:    windowRange(),
	}, fetchAWSOptions{
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err == nil || !strings.Contains(err.Error(), "reconcil") {
		t.Fatalf("err = %v, want reconciliation error", err)
	}
}

func TestFetchMonthlyAccountServiceReconciliationWithinTolerance(t *testing.T) {
	ce := &routingCE{pages: [][]types.ResultByTime{
		{groupedRow("2026-06", testMemberA, "Amazon EC2", "100.005", "USD")},
		{groupedRow("2026-06", testPayerID, "Amazon S3", "25", "USD")},
		{totalRow("2026-06", "125.01", "USD")},
	}}
	res, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: payerMemberTargets(),
		Range:    windowRange(),
	}, fetchAWSOptions{
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatalf("sub-cent difference should pass: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(res.Rows))
	}
}

func TestFetchMonthlyAccountServicePayerSpendMissingFails(t *testing.T) {
	// Totals include payer spend (150) but the payer filtered query returns nothing.
	ce := &routingCE{pages: [][]types.ResultByTime{
		{groupedRow("2026-06", testMemberA, "Amazon EC2", "100", "USD")},
		{},
		{totalRow("2026-06", "150", "USD")},
	}}
	_, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: payerMemberTargets(),
		Range:    windowRange(),
	}, fetchAWSOptions{
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err == nil || !strings.Contains(err.Error(), "reconcil") {
		t.Fatalf("err = %v, want reconciliation error for missing payer spend", err)
	}
}

func TestFetchMonthlyAccountServiceMixedCurrencyAcrossQueriesFails(t *testing.T) {
	ce := &routingCE{pages: [][]types.ResultByTime{
		{groupedRow("2026-06", testMemberA, "Amazon EC2", "100", "USD")},
		{groupedRow("2026-06", testPayerID, "Amazon S3", "25", "EUR")},
		{totalRow("2026-06", "125", "USD")},
	}}
	_, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: payerMemberTargets(),
		Range:    windowRange(),
	}, fetchAWSOptions{
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err == nil || !strings.Contains(err.Error(), "currency") {
		t.Fatalf("err = %v, want mixed currency error", err)
	}
}

func TestFetchMonthlyAccountServiceMixedCredentialsFails(t *testing.T) {
	targets := payerMemberTargets()
	targets[1].PayerAccountID = "999999999999"
	_, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: targets,
		Range:    windowRange(),
	}, fetchAWSOptions{
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return &routingCE{} },
	})
	if err == nil || !strings.Contains(err.Error(), "payer") {
		t.Fatalf("err = %v, want single-payer error", err)
	}
}

func TestFetchMonthlyAccountServiceNoPayerFails(t *testing.T) {
	_, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: []AccountTarget{
			{AccountID: testMemberA, PayerAccountID: testPayerID, AWSConfig: aws.Config{}},
		},
		Range: windowRange(),
	}, fetchAWSOptions{
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return &routingCE{} },
	})
	if err == nil || !strings.Contains(err.Error(), "payer") {
		t.Fatalf("err = %v, want payer target required error", err)
	}
}

func TestFetchMonthlyAccountServiceEmptyQueryDoesNotForceUSDCurrency(t *testing.T) {
	// Non-USD org where the payer has no own spend: the empty payer query must
	// not introduce a default USD currency that conflicts with EUR member rows.
	ce := &routingCE{pages: [][]types.ResultByTime{
		// member batch (EUR)
		{groupedRow("2026-06", testMemberA, "Amazon EC2", "100", "EUR")},
		// payer query: no rows
		{},
		// totals (EUR)
		{totalRow("2026-06", "100", "EUR")},
	}}
	res, err := fetchMonthlyAccountServiceWith(context.Background(), CostQuery{
		Provider: ProviderAWS,
		Accounts: payerMemberTargets(),
		Range:    windowRange(),
	}, fetchAWSOptions{
		NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Currency != "EUR" {
		t.Errorf("currency = %q, want EUR", res.Currency)
	}
}
