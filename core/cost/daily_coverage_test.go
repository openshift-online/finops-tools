package cost

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

func TestDailyCoverageRetainsZeroAndRejectsMissingDays(t *testing.T) {
	dr := DateRange{Start: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)}
	row := func(date, amount string) types.ResultByTime {
		return types.ResultByTime{TimePeriod: &types.DateInterval{Start: aws.String(date)}, Total: map[string]types.MetricValue{MetricNetAmortized: {Amount: aws.String(amount), Unit: aws.String("USD")}}}
	}
	for _, bulk := range []bool{false, true} {
		for _, missing := range []bool{false, true} {
			name := "single"
			if bulk {
				name = "bulk"
			}
			if missing {
				name += "/missing"
			} else {
				name += "/zero"
			}
			t.Run(name, func(t *testing.T) {
				ce := &fakeCE{pages: [][]types.ResultByTime{{row("2026-05-01", "0")}, {row("2026-05-02", "10")}}}
				if missing {
					ce.pages = ce.pages[:1]
				}
				opts := fetchAWSOptions{NewCostExplorer: func(aws.Config) CostExplorerAPI { return ce }}
				targets := []AccountTarget{{AccountID: "111111111111", PayerAccountID: "123456789012"}}
				q := CostQuery{Accounts: targets, Range: dr, RequireDailyCoverage: true}
				var daily []DailyCostItem
				var err error
				if bulk {
					targets = append(targets, AccountTarget{AccountID: "222222222222", PayerAccountID: "123456789012"})
					daily, _, err = fetchAWSDailyNetAmortizedBulk(t.Context(), q, targets, opts)
				} else {
					daily, _, err = fetchAWSDailyNetAmortizedWith(t.Context(), q, opts)
				}
				if missing {
					if err == nil {
						t.Fatal("missing day must not become zero spend")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(daily) != 2 || daily[0].Date != "2026-05-01" || daily[0].Amount != 0 || daily[1].Amount != 10 {
					t.Fatalf("daily = %+v", daily)
				}
			})
		}
	}
}

func TestMergeDailyPreservesZeroDaysWhenRequired(t *testing.T) {
	series := [][]DailyCostItem{{{Date: "2026-05-01", Amount: 10}}, {{Date: "2026-05-01", Amount: -10}}}
	got := mergeDaily(series, true)
	if len(got) != 1 || got[0].Amount != 0 {
		t.Fatalf("zero-sum date lost: %+v", got)
	}
	if got := MergeDaily(series); len(got) != 0 {
		t.Fatalf("default behavior changed: %+v", got)
	}
}
