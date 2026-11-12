package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/cost"
)

func supportCostGroup(id, amount, currency string) cetypes.Group {
	return cetypes.Group{Keys: []string{id}, Metrics: map[string]cetypes.MetricValue{
		"NetUnblendedCost": {Amount: aws.String(amount), Unit: aws.String(currency)},
	}}
}

func supportCostBucket(groups ...cetypes.Group) cetypes.ResultByTime {
	return cetypes.ResultByTime{TimePeriod: &cetypes.DateInterval{Start: aws.String("2026-09-20"), End: aws.String("2026-09-21")}, Groups: groups}
}

func supportCostQuery(api BillingAPI) CostQuery {
	return CostQuery{Targets: []cost.AccountTarget{{AccountID: "123456789012"}}, Range: billingTestPeriod(),
		newClient: func(cost.AccountTarget) BillingAPI { return api }}
}

func TestSupportCostsBillingFiltersPaginationAndGrouping(t *testing.T) {
	for _, grouping := range []CostGroupBy{CostGroupByService, CostGroupByAccount, CostGroupByServiceAccount} {
		t.Run(string(grouping), func(t *testing.T) {
			dimensionCalls, usageCalls := 0, 0
			api := fakeBillingAPI{
				dimensions: func(_ context.Context, in *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
					dimensionCalls++
					if aws.ToString(in.TimePeriod.Start) != "2026-09-20" || aws.ToString(in.TimePeriod.End) != "2026-10-20" {
						t.Fatalf("period: %+v", in.TimePeriod)
					}
					if in.NextPageToken == nil {
						return &costexplorer.GetDimensionValuesOutput{NextPageToken: aws.String("next")}, nil
					}
					return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("BoxUsage", "USE1-ExtendedSupport", "USE1-ExtendedSupport")}, nil
				},
				usage: func(_ context.Context, in *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
					usageCalls++
					if in.Granularity != cetypes.GranularityDaily || !reflect.DeepEqual(in.Metrics, []string{"NetUnblendedCost"}) || aws.ToString(in.GroupBy[0].Key) != "LINKED_ACCOUNT" {
						t.Fatalf("request: %+v", in)
					}
					if len(in.Filter.And) != 3 {
						t.Fatalf("filter: %+v", in.Filter)
					}
					if !reflect.DeepEqual(in.Filter.And[0].Dimensions.Values, []string{"USE1-ExtendedSupport"}) ||
						!reflect.DeepEqual(in.Filter.And[2].Dimensions.Values, []string{"111111111111", "222222222222"}) {
						t.Fatalf("filter: %+v", in.Filter)
					}
					service := in.Filter.And[1].Dimensions.Values[0]
					amount := "12.50"
					if service == "Amazon Elastic Kubernetes Service" {
						amount = "6"
					}
					bucket := supportCostBucket(supportCostGroup("111111111111", amount, "USD"))
					out := &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{bucket}}
					if in.NextPageToken == nil {
						out.NextPageToken = aws.String("page2")
					} else {
						out.ResultsByTime[0].Groups = []cetypes.Group{supportCostGroup("222222222222", "-0.50", "USD")}
						out.ResultsByTime[0].Estimated = true
					}
					return out, nil
				},
			}
			q := supportCostQuery(api)
			q.Granularity = "daily"
			q.GroupBy = grouping
			q.Targets = []cost.AccountTarget{
				{AccountID: "111111111111", PayerAccountID: "123456789012"},
				{AccountID: "222222222222", PayerAccountID: "123456789012"},
				{AccountID: "111111111111", PayerAccountID: "123456789012"},
			}
			result, err := FetchCosts(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			if dimensionCalls != 2 || usageCalls != 8 || result.Amount != 41.5 || result.KnownAvoidableAmount != 36 || result.AvoidableAmount != nil || !result.Estimated || result.Currency != "USD" {
				t.Fatalf("result=%+v dimensions=%d usage=%d", result, dimensionCalls, usageCalls)
			}
			if len(result.ServiceTotals) != 4 {
				t.Fatalf("missing service totals: %+v", result.ServiceTotals)
			}
			for i, total := range result.ServiceTotals {
				want := 12.0
				if total.Service == ServiceEKS {
					want = 5.5
				}
				if total.Amount != want || total.StartDate != "2026-09-20" || total.EndDate != "2026-10-19" || total.AccountID != "" || !total.Estimated || (total.AvoidableAmount == nil) != (total.Service == ServiceEKS) {
					t.Fatalf("service total=%+v", total)
				}
				if i > 0 && result.ServiceTotals[i-1].Service >= total.Service {
					t.Fatalf("service totals are not sorted and unique: %+v", result.ServiceTotals)
				}
			}
			wantRows := map[CostGroupBy]int{CostGroupByService: 4, CostGroupByAccount: 2, CostGroupByServiceAccount: 8}[grouping]
			if len(result.Rows) != wantRows || !reflect.DeepEqual(result.UsageTypes, []string{"USE1-ExtendedSupport"}) {
				t.Fatalf("result=%+v", result)
			}
			for _, row := range result.Rows {
				if row.EndDate != "2026-09-20" || (grouping == CostGroupByAccount && row.Service != "") || (grouping == CostGroupByService && row.AccountID != "") {
					t.Fatalf("row=%+v", row)
				}
				if (row.Service == ServiceEKS || grouping == CostGroupByAccount) && row.AvoidableAmount != nil {
					t.Fatalf("EKS baseline treated as avoidable: %+v", row)
				}
			}
		})
	}
}

func TestSupportCostsPayerIncludesHistoricalAccountsAndBatches(t *testing.T) {
	var values []string
	for i := 0; i < 101; i++ {
		values = append(values, fmt.Sprintf("ExtendedSupport-%03d", i))
	}
	queries := 0
	api := fakeBillingAPI{
		dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
			return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions(values...)}, nil
		},
		usage: func(_ context.Context, in *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
			queries++
			if len(in.Filter.And) != 2 || len(in.Filter.And[0].Dimensions.Values) > 100 {
				t.Fatalf("filter: %+v", in.Filter)
			}
			// This account need not exist in today's Organizations inventory.
			return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{supportCostBucket(supportCostGroup("999999999999", "4", "USD"))}}, nil
		},
	}
	q := supportCostQuery(api)
	q.Services = []Service{ServiceRDS, ServiceRDS}
	q.Targets = append(q.Targets, cost.AccountTarget{AccountID: "111111111111", PayerAccountID: "123456789012"})
	r, err := FetchCosts(context.Background(), q)
	if err != nil || queries != 2 || r.Amount != 8 || r.AvoidableAmount == nil || *r.AvoidableAmount != 8 {
		t.Fatalf("result=%+v calls=%d err=%v", r, queries, err)
	}
}

func TestSupportCostsSeparatesCredentialSources(t *testing.T) {
	var sources []string
	q := supportCostQuery(nil)
	q.Services = []Service{ServiceRDS}
	q.Targets = []cost.AccountTarget{{AccountID: "123456789012"}, {AccountID: "987654321098"}}
	q.newClient = func(target cost.AccountTarget) BillingAPI {
		sources = append(sources, target.CredentialsAccountID())
		return fakeBillingAPI{
			dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
				return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
			},
			usage: func(context.Context, *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
				return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{supportCostBucket(supportCostGroup(target.AccountID, "2", "USD"))}}, nil
			},
		}
	}
	r, err := FetchCosts(context.Background(), q)
	if err != nil || r.Amount != 4 || !reflect.DeepEqual(sources, []string{"123456789012", "987654321098"}) {
		t.Fatalf("result=%+v sources=%v err=%v", r, sources, err)
	}
}

func TestSupportCostsMonthlyBucketsAndLargeAccountSelection(t *testing.T) {
	requests := 0
	seenAccounts := make(map[string]bool)
	api := fakeBillingAPI{
		dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
			return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
		},
		usage: func(_ context.Context, in *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
			requests++
			if in.Granularity != cetypes.GranularityMonthly {
				t.Fatalf("granularity=%s", in.Granularity)
			}
			ids := in.Filter.And[2].Dimensions.Values
			if len(ids) > 100 {
				t.Fatalf("oversize batch: %d", len(ids))
			}
			var groups []cetypes.Group
			for _, id := range ids {
				if seenAccounts[id] {
					t.Fatalf("account queried twice: %s", id)
				}
				seenAccounts[id] = true
				groups = append(groups, supportCostGroup(id, "0.25", "USD"))
			}
			// A calendar month can be clipped at both requested period boundaries.
			return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{
				{TimePeriod: &cetypes.DateInterval{Start: aws.String("2026-09-20"), End: aws.String("2026-10-01")}, Groups: groups},
				{TimePeriod: &cetypes.DateInterval{Start: aws.String("2026-10-01"), End: aws.String("2026-10-20")}, Groups: groups},
			}}, nil
		},
	}
	q := supportCostQuery(api)
	q.Services = []Service{ServiceRDS}
	q.Granularity = "monthly"
	q.Targets = nil
	for i := 0; i < 201; i++ {
		q.Targets = append(q.Targets, cost.AccountTarget{AccountID: fmt.Sprintf("%012d", i), PayerAccountID: "123456789012"})
	}
	r, err := FetchCosts(context.Background(), q)
	if err != nil || requests != 3 || len(seenAccounts) != 201 || len(r.Rows) != 2 || r.Amount != 100.5 {
		t.Fatalf("result=%+v requests=%d err=%v", r, requests, err)
	}
	if r.Rows[0].StartDate != "2026-09-20" || r.Rows[0].EndDate != "2026-09-30" || r.Rows[1].StartDate != "2026-10-01" || r.Rows[1].EndDate != "2026-10-19" {
		t.Fatalf("monthly buckets=%+v", r.Rows)
	}
}

func TestSupportCostTotalsCombineMonthsAndPreserveGroupAmounts(t *testing.T) {
	for _, grouping := range []CostGroupBy{CostGroupByService, CostGroupByAccount, CostGroupByServiceAccount} {
		t.Run(string(grouping), func(t *testing.T) {
			requests := 0
			api := fakeBillingAPI{
				dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
					return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
				},
				usage: func(_ context.Context, in *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
					requests++
					if in.Granularity != cetypes.GranularityMonthly {
						t.Fatalf("invalid billing granularity: %s", in.Granularity)
					}
					first := []cetypes.Group{supportCostGroup("111111111111", "10", "USD"), supportCostGroup("222222222222", "5", "USD")}
					last := []cetypes.Group{supportCostGroup("111111111111", "20", "USD"), supportCostGroup("222222222222", "-1", "USD")}
					if in.Filter.And[1].Dimensions.Values[0] == "Amazon Elastic Kubernetes Service" {
						first = []cetypes.Group{supportCostGroup("111111111111", "6", "USD")}
						last = []cetypes.Group{supportCostGroup("111111111111", "12", "USD")}
					}
					return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{
						{TimePeriod: &cetypes.DateInterval{Start: aws.String("2026-09-20"), End: aws.String("2026-10-01")}, Groups: first},
						{TimePeriod: &cetypes.DateInterval{Start: aws.String("2026-10-01"), End: aws.String("2026-10-20")}, Groups: last, Estimated: true},
					}}, nil
				},
			}
			q := supportCostQuery(api)
			q.Services, q.GroupBy, q.TotalsOnly = []Service{ServiceRDS, ServiceEKS}, grouping, true
			r, err := FetchCosts(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			wantRows := map[CostGroupBy]int{CostGroupByService: 2, CostGroupByAccount: 2, CostGroupByServiceAccount: 3}[grouping]
			if r.Granularity != "total" || len(r.Rows) != wantRows || r.Amount != 52 || r.KnownAvoidableAmount != 34 || r.AvoidableAmount != nil || !r.Estimated {
				t.Fatalf("totals=%+v", r)
			}
			if len(r.ServiceTotals) != 0 {
				t.Fatalf("totals-only view duplicated service totals: %+v", r.ServiceTotals)
			}
			for _, row := range r.Rows {
				if row.StartDate != "2026-09-20" || row.EndDate != "2026-10-19" || !row.Estimated {
					t.Fatalf("row=%+v", row)
				}
				want := 0.0
				switch grouping {
				case CostGroupByService:
					want = map[Service]float64{ServiceRDS: 34, ServiceEKS: 18}[row.Service]
				case CostGroupByAccount:
					want = map[string]float64{"111111111111": 48, "222222222222": 4}[row.AccountID]
				case CostGroupByServiceAccount:
					want = map[string]float64{"rds/111111111111": 30, "rds/222222222222": 4, "eks/111111111111": 18}[string(row.Service)+"/"+row.AccountID]
				}
				if row.Amount != want {
					t.Fatalf("group amount=%+v, want %v", row, want)
				}
			}

			q.TotalsOnly = false
			r, err = FetchCosts(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			if requests != 4 || r.Granularity != "monthly" || len(r.Rows) != 2*wantRows || r.Amount != 52 || len(r.ServiceTotals) != 2 {
				t.Fatalf("monthly breakdown=%+v requests=%d", r, requests)
			}
			for i, total := range r.ServiceTotals {
				wantService := []Service{ServiceEKS, ServiceRDS}[i]
				wantAmount := []float64{18, 34}[i]
				if total.Service != wantService || total.Amount != wantAmount || total.StartDate != "2026-09-20" || total.EndDate != "2026-10-19" || total.AccountID != "" || !total.Estimated {
					t.Fatalf("monthly service total=%+v", total)
				}
				if total.Service == ServiceEKS {
					if total.AvoidableAmount != nil || total.KnownAvoidableAmount != 0 {
						t.Fatalf("EKS avoidable subtotal=%+v", total)
					}
				} else if total.AvoidableAmount == nil || *total.AvoidableAmount != 34 || total.KnownAvoidableAmount != 34 {
					t.Fatalf("RDS avoidable subtotal=%+v", total)
				}
			}
		})
	}
}

func TestSupportCostsNeverTurnsIncompleteBillingIntoZero(t *testing.T) {
	for _, stage := range []string{"dimension failure", "page failure", "cycle", "nil response", "invalid account", "outside scope", "missing metric", "invalid amount", "nonfinite", "missing currency", "mixed currency", "missing period", "out of range", "cancellation"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			api := fakeBillingAPI{
				dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
					if stage == "dimension failure" {
						return nil, errors.New("denied")
					}
					return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
				},
				usage: func(_ context.Context, in *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
					bucket := supportCostBucket(supportCostGroup("111111111111", "1", "USD"))
					switch stage {
					case "page failure":
						if in.NextPageToken != nil {
							return nil, errors.New("page failed")
						}
					case "nil response":
						return nil, nil
					case "invalid account":
						bucket.Groups[0].Keys = []string{"not-an-account"}
					case "outside scope":
						bucket.Groups[0].Keys = []string{"999999999999"}
					case "missing metric":
						bucket.Groups[0].Metrics = nil
					case "invalid amount":
						bucket.Groups[0] = supportCostGroup("111111111111", "bad", "USD")
					case "nonfinite":
						bucket.Groups[0] = supportCostGroup("111111111111", "NaN", "USD")
					case "missing currency":
						bucket.Groups[0] = supportCostGroup("111111111111", "1", "")
					case "mixed currency":
						bucket.Groups = append(bucket.Groups, supportCostGroup("111111111111", "1", "EUR"))
					case "missing period":
						bucket.TimePeriod = nil
					case "out of range":
						bucket.TimePeriod.Start = aws.String("2020-01-01")
					case "cancellation":
						cancel()
						return nil, ctx.Err()
					}
					out := &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{bucket}}
					if stage == "page failure" || stage == "cycle" {
						out.NextPageToken = aws.String("same")
					}
					return out, nil
				},
			}
			q := supportCostQuery(api)
			q.Targets = []cost.AccountTarget{{AccountID: "111111111111", PayerAccountID: "123456789012"}}
			q.Services = []Service{ServiceRDS}
			r, err := FetchCosts(ctx, q)
			if err == nil || len(r.Rows) != 0 || r.Amount != 0 || r.Coverage != nil {
				t.Fatalf("partial result=%+v err=%v", r, err)
			}
			if stage == "cancellation" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestSupportCostsEmptyAndExplicitMetric(t *testing.T) {
	api := fakeBillingAPI{dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
		return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ordinary")}, nil
	}, usage: func(context.Context, *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
		t.Fatal("unrestricted cost query")
		return nil, nil
	}}
	r, err := FetchCosts(context.Background(), supportCostQuery(api))
	if err != nil || r.Rows == nil || len(r.Rows) != 0 || r.Amount != 0 || r.AvoidableAmount == nil || *r.AvoidableAmount != 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	api.dimensions = func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
		return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
	}
	api.usage = func(_ context.Context, in *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
		if !reflect.DeepEqual(in.Metrics, []string{"UnblendedCost"}) {
			t.Fatal(in.Metrics)
		}
		group := supportCostGroup("111111111111", "0", "USD")
		group.Metrics["UnblendedCost"] = cetypes.MetricValue{Amount: aws.String("0"), Unit: aws.String("USD")}
		return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{supportCostBucket(group)}}, nil
	}
	q := supportCostQuery(api)
	q.Metric = "UnblendedCost"
	q.Services = []Service{ServiceRDS}
	r, err = FetchCosts(context.Background(), q)
	if err != nil || r.Metric != "UnblendedCost" || len(r.Rows) != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestSupportCostQueryValidation(t *testing.T) {
	for _, value := range []string{"rds", "none", "service,rds"} {
		if _, err := ParseCostGroupBy(value); err == nil {
			t.Fatalf("accepted grouping %s", value)
		}
	}
	for _, value := range []string{"hourly", "yearly"} {
		if _, err := ParseCostGranularity(value); err == nil {
			t.Fatalf("accepted granularity %s", value)
		}
	}
	for _, change := range []func(*CostQuery){
		func(q *CostQuery) { q.Targets = nil },
		func(q *CostQuery) { q.Targets[0].AccountID = "bad" },
		func(q *CostQuery) { q.Services = []Service{"ec2"} },
		func(q *CostQuery) { q.Range = cost.DateRange{} },
		func(q *CostQuery) { q.Metric = "UsageQuantity" },
		func(q *CostQuery) { q.allServices = true; q.GroupBy = CostGroupByService },
	} {
		q := supportCostQuery(nil)
		change(&q)
		if _, err := FetchCosts(context.Background(), q); err == nil {
			t.Fatal("accepted invalid query")
		}
	}
	// A selected payer account remains scoped even alongside a member selection.
	filter := costAccountFilter([]cost.AccountTarget{{AccountID: "123456789012", ScopeAccountOnly: true}, {AccountID: "111111111111", PayerAccountID: "123456789012"}})
	if filter == nil || strings.Join(filter.Dimensions.Values, ",") != "111111111111,123456789012" {
		t.Fatalf("filter=%+v", filter)
	}
}
