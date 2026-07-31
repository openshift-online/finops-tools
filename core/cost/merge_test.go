// merge_test.go tests merging multiple CostResult values and breakdown aggregation.
package cost

import "testing"

func TestMergeResultsCombinesTotalsAndServices(t *testing.T) {
	results := []CostResult{
		{
			Provider: ProviderAWS, AccountName: "a", AccountID: "1", Currency: "USD",
			StartDate: "2026-04-25", EndDate: "2026-05-24", Amount: 100,
			Breakdown: []CostBreakdownItem{{Service: "Amazon EC2", Amount: 80}, {Service: "Amazon S3", Amount: 20}},
		},
		{
			Provider: ProviderAWS, AccountName: "b", AccountID: "2", Currency: "USD",
			StartDate: "2026-04-25", EndDate: "2026-05-24", Amount: 50,
			Breakdown: []CostBreakdownItem{{Service: "Amazon EC2", Amount: 30}, {Service: "Amazon RDS", Amount: 20}},
		},
	}

	merged, err := MergeResults(results)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Amount != 150 {
		t.Errorf("Amount = %v, want 150", merged.Amount)
	}
	if merged.AccountName != "a, b" {
		t.Errorf("AccountName = %q", merged.AccountName)
	}
	if len(merged.Breakdown) != 3 {
		t.Fatalf("Breakdown = %+v", merged.Breakdown)
	}
	if merged.Breakdown[0].Service != "Amazon EC2" || merged.Breakdown[0].Amount != 110 {
		t.Errorf("EC2 = %+v", merged.Breakdown[0])
	}
}

func TestMergeResultsPreservesAccountNames(t *testing.T) {
	results := []CostResult{
		{
			Provider: ProviderAWS, Currency: "USD",
			StartDate: "2026-04-25", EndDate: "2026-05-24", Amount: 60, GroupBy: GroupByAccount,
			Breakdown: []CostBreakdownItem{{Account: "111111111111", AccountName: "Member One", Amount: 60}},
		},
		{
			Provider: ProviderAWS, Currency: "USD",
			StartDate: "2026-04-25", EndDate: "2026-05-24", Amount: 40, GroupBy: GroupByAccount,
			Breakdown: []CostBreakdownItem{{Account: "222222222222", AccountName: "Member Two", Amount: 40}},
		},
	}
	merged, err := MergeResults(results)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Breakdown) != 2 {
		t.Fatalf("breakdown = %+v", merged.Breakdown)
	}
	if merged.Breakdown[0].AccountName != "Member One" || merged.Breakdown[1].AccountName != "Member Two" {
		t.Fatalf("names lost: %+v", merged.Breakdown)
	}
}

func TestMergeResultsCombinesLinkedAccounts(t *testing.T) {
	results := []CostResult{
		{
			Provider: ProviderAWS, AccountName: "payer-a", Currency: "USD",
			StartDate: "2026-04-25", EndDate: "2026-05-24", Amount: 60, GroupBy: GroupByAccount,
			Breakdown: []CostBreakdownItem{{Account: "111111111111", Amount: 60}},
		},
		{
			Provider: ProviderAWS, AccountName: "payer-b", Currency: "USD",
			StartDate: "2026-04-25", EndDate: "2026-05-24", Amount: 40, GroupBy: GroupByAccount,
			Breakdown: []CostBreakdownItem{{Account: "111111111111", Amount: 10}, {Account: "222222222222", Amount: 30}},
		},
	}
	merged, err := MergeResults(results)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Amount != 100 {
		t.Errorf("Amount = %v", merged.Amount)
	}
	if len(merged.Breakdown) != 2 || merged.Breakdown[0].Account != "111111111111" || merged.Breakdown[0].Amount != 70 {
		t.Fatalf("Breakdown = %+v", merged.Breakdown)
	}
}

func TestMergeDaily(t *testing.T) {
	merged := MergeDaily([][]DailyCostItem{
		{{Date: "2026-05-24", Amount: 10}},
		{{Date: "2026-05-24", Amount: 5}, {Date: "2026-05-23", Amount: 3}},
	})
	if len(merged) != 2 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged[0].Date != "2026-05-23" || merged[0].Amount != 3 {
		t.Errorf("first = %+v", merged[0])
	}
	if merged[1].Date != "2026-05-24" || merged[1].Amount != 15 {
		t.Errorf("second = %+v", merged[1])
	}
}

func TestMergeResultsRejectsMixedCurrency(t *testing.T) {
	_, err := MergeResults([]CostResult{
		{Currency: "USD", StartDate: "2026-04-25", EndDate: "2026-05-24", Amount: 1},
		{Currency: "EUR", StartDate: "2026-04-25", EndDate: "2026-05-24", Amount: 1},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
