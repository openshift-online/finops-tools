package extendedsupport

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/cost"
)

func TestSupportCostCoveragePreservesTwelveMonthGapsAndZeroRecords(t *testing.T) {
	for _, totalsOnly := range []bool{false, true} {
		name := "monthly breakdown"
		if totalsOnly {
			name = "period totals"
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			bucket := func(start time.Time, groups ...cetypes.Group) cetypes.ResultByTime {
				return cetypes.ResultByTime{
					TimePeriod: &cetypes.DateInterval{Start: aws.String(cost.FormatDate(start)), End: aws.String(cost.FormatDate(start.AddDate(0, 1, 0)))},
					Groups:     groups,
				}
			}
			api := fakeBillingAPI{
				dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
					return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
				},
				usage: func(_ context.Context, in *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
					calls++
					if in.Granularity != cetypes.GranularityMonthly {
						t.Fatalf("unexpected request granularity: %s", in.Granularity)
					}
					// October is omitted. November and December are returned empty.
					// January is initially empty, with a zero record on the next page.
					if in.NextPageToken == nil {
						return &costexplorer.GetCostAndUsageOutput{NextPageToken: aws.String("next"), ResultsByTime: []cetypes.ResultByTime{
							bucket(time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)),
							bucket(time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)),
							bucket(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
						}}, nil
					}
					out := &costexplorer.GetCostAndUsageOutput{}
					for month := time.January; month <= time.September; month++ {
						amount := "100"
						if month == time.January {
							amount = "0"
						}
						out.ResultsByTime = append(out.ResultsByTime, bucket(time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC), supportCostGroup("111111111111", amount, "USD")))
					}
					return out, nil
				},
			}
			q := supportCostQuery(api)
			q.Range = cost.DateRange{Start: time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
			q.Services, q.TotalsOnly = []Service{ServiceRDS}, totalsOnly
			r, err := FetchCosts(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || r.Amount != 800 || r.Coverage == nil || r.Coverage.Granularity != "monthly" || r.Coverage.TotalPeriods != 12 || r.Coverage.PeriodsWithRecords != 9 || len(r.Coverage.Periods) != 12 {
				t.Fatalf("result=%+v coverage=%+v calls=%d", r, r.Coverage, calls)
			}
			for i, period := range r.Coverage.Periods {
				want := CostPeriodMatchingCharges
				if i == 0 {
					want = CostPeriodNotReturned
				} else if i < 3 {
					want = CostPeriodNoMatchingCharges
				}
				if period.Status != want || period.Incomplete {
					t.Fatalf("period %d=%+v, want %s", i, period, want)
				}
			}
			if r.Coverage.Periods[0].StartDate != "2025-10-01" || r.Coverage.Periods[11].EndDate != "2026-09-30" {
				t.Fatalf("coverage boundaries=%+v", r.Coverage)
			}
			if totalsOnly {
				if r.Granularity != "total" || len(r.Rows) != 1 || r.Rows[0].StartDate != "2025-10-01" || r.Rows[0].EndDate != "2026-09-30" {
					t.Fatalf("period totals=%+v", r)
				}
			} else if len(r.Rows) != 9 || r.Rows[0].StartDate != "2026-01-01" || r.Rows[0].Amount != 0 {
				t.Fatalf("explicit zero record lost: %+v", r.Rows)
			}
		})
	}
}

func TestSupportCostDailyCoverageCombinesPayersAndFlagsPartialResponses(t *testing.T) {
	calls := 0
	q := supportCostQuery(nil)
	q.Range = cost.DateRange{Start: time.Date(2024, 2, 28, 0, 0, 0, 0, time.UTC), End: time.Date(2024, 3, 3, 0, 0, 0, 0, time.UTC)}
	q.Services, q.GroupBy, q.Granularity = []Service{ServiceRDS}, CostGroupByAccount, "daily"
	q.Targets = []cost.AccountTarget{{AccountID: "111111111111"}, {AccountID: "222222222222"}}
	q.newClient = func(target cost.AccountTarget) BillingAPI {
		return fakeBillingAPI{
			dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
				return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
			},
			usage: func(context.Context, *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
				calls++
				bucket := func(start string, groups ...cetypes.Group) cetypes.ResultByTime {
					day, err := cost.ParseDate(start)
					if err != nil {
						t.Fatal(err)
					}
					return cetypes.ResultByTime{TimePeriod: &cetypes.DateInterval{Start: aws.String(start), End: aws.String(cost.FormatDate(day.AddDate(0, 0, 1)))}, Groups: groups}
				}
				if target.AccountID == "111111111111" {
					return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{
						bucket("2024-02-28", supportCostGroup(target.AccountID, "-10", "USD")),
						bucket("2024-02-29"), bucket("2024-03-01"),
					}}, nil
				}
				return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{
					bucket("2024-02-29", supportCostGroup(target.AccountID, "0", "USD")),
				}}, nil
			},
		}
	}
	r, err := FetchCosts(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || r.Amount != -10 || len(r.Rows) != 2 || r.Coverage == nil || r.Coverage.TotalPeriods != 4 || r.Coverage.PeriodsWithRecords != 2 || r.Coverage.Granularity != "daily" {
		t.Fatalf("result=%+v coverage=%+v calls=%d", r, r.Coverage, calls)
	}
	want := []CostPeriodCoverage{
		{StartDate: "2024-02-28", EndDate: "2024-02-28", Status: CostPeriodMatchingCharges, Incomplete: true},
		{StartDate: "2024-02-29", EndDate: "2024-02-29", Status: CostPeriodMatchingCharges},
		{StartDate: "2024-03-01", EndDate: "2024-03-01", Status: CostPeriodNoMatchingCharges, Incomplete: true},
		{StartDate: "2024-03-02", EndDate: "2024-03-02", Status: CostPeriodNotReturned},
	}
	for i, period := range r.Coverage.Periods {
		if period != want[i] {
			t.Fatalf("period %d=%+v, want %+v", i, period, want[i])
		}
	}
}

func TestSupportCostCoverageWhenBillingIsNotQueried(t *testing.T) {
	for _, excluded := range []bool{false, true} {
		q := supportCostQuery(fakeBillingAPI{
			dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
				if excluded {
					t.Fatal("excluded scope queried billing metadata")
				}
				return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ordinary")}, nil
			},
			usage: func(context.Context, *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
				t.Fatal("coverage triggered an unnecessary billing query")
				return nil, nil
			},
		})
		if excluded {
			q.Regions, q.SkipRegions = []string{"us-east-1"}, []string{"us-east-1"}
		}
		r, err := FetchCosts(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if excluded {
			if r.Coverage != nil {
				t.Fatalf("excluded scope reported coverage: %+v", r.Coverage)
			}
			continue
		}
		if r.Coverage == nil || r.Coverage.TotalPeriods != 2 || r.Coverage.PeriodsWithRecords != 0 {
			t.Fatalf("coverage=%+v", r.Coverage)
		}
		if r.Coverage.Periods[0].StartDate != "2026-09-20" || r.Coverage.Periods[0].EndDate != "2026-09-30" || r.Coverage.Periods[1].EndDate != "2026-10-19" {
			t.Fatalf("partial months were not clipped to the requested range: %+v", r.Coverage.Periods)
		}
		for _, period := range r.Coverage.Periods {
			if period.Status != CostPeriodNotQueried || period.Incomplete {
				t.Fatalf("metadata-only query invented coverage: %+v", period)
			}
		}
	}
}

func TestSupportCostCoverageEmptyResponseDoesNotEstablishZeroHistory(t *testing.T) {
	q := supportCostQuery(fakeBillingAPI{
		dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
			return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
		},
		usage: func(context.Context, *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
			return &costexplorer.GetCostAndUsageOutput{}, nil
		},
	})
	q.Services = []Service{ServiceRDS}
	r, err := FetchCosts(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 0 || r.Coverage == nil || r.Coverage.TotalPeriods != 2 || r.Coverage.PeriodsWithRecords != 0 {
		t.Fatalf("empty response=%+v", r)
	}
	for _, period := range r.Coverage.Periods {
		if period.Status != CostPeriodNotReturned {
			t.Fatalf("empty response was classified as zero cost: %+v", period)
		}
	}
}
