package extendedsupport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/cost"
)

var listCostTestNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func listCostRecord(account string, service Service, id string) Record {
	return Record{AccountID: account, Service: service, ResourceID: id, Region: "us-east-1"}
}

func listCostInventory(records ...Record) Result {
	return buildResult(records, nil, nil, nil, listCostTestNow)
}

func listCostTestBucket(groups ...cetypes.Group) cetypes.ResultByTime {
	return cetypes.ResultByTime{TimePeriod: &cetypes.DateInterval{Start: aws.String("2026-11-08T00:00:00Z"), End: aws.String("2026-11-09T00:00:00Z")}, Groups: groups}
}

func TestListCostsSDKUsesAccountBillingAndAddsBothSummaries(t *testing.T) {
	requests := 0
	config := aws.Config{
		Region: "us-west-2", Credentials: credentials.NewStaticCredentialsProvider("payer", "secret", "token"),
		HTTPClient: billingHTTPClient(func(request *http.Request) (*http.Response, error) {
			requests++
			if !strings.Contains(request.URL.Host, "us-east-1") || !strings.Contains(request.Header.Get("Authorization"), "Credential=payer/") {
				t.Fatal("cost lookup did not use payer credentials and billing endpoint")
			}
			var input struct {
				TimePeriod  struct{ Start, End string }
				Granularity string
				Metrics     []string
				GroupBy     []struct{ Type, Key string }
				Filter      struct {
					And []struct {
						Dimensions struct {
							Key    string
							Values []string
						}
					}
				}
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.TimePeriod.Start != "2026-10-10" || input.TimePeriod.End != "2026-11-09" {
				t.Fatalf("window=%+v", input.TimePeriod)
			}
			body := `{"DimensionValues":[{"Value":"BoxUsage"},{"Value":"USE1-ExtendedSupport"}]}`
			switch operation := request.Header.Get("X-Amz-Target"); operation {
			case "AWSInsightsIndexService.GetDimensionValues":
			case "AWSInsightsIndexService.GetCostAndUsage":
				if input.Granularity != "DAILY" || !reflect.DeepEqual(input.Metrics, []string{"NetUnblendedCost"}) || len(input.GroupBy) != 1 || input.GroupBy[0].Key != "LINKED_ACCOUNT" {
					t.Fatalf("request=%+v", input)
				}
				if len(input.Filter.And) != 4 || !reflect.DeepEqual(input.Filter.And[0].Dimensions.Values, []string{"USE1-ExtendedSupport"}) || input.Filter.And[2].Dimensions.Key != "LINKED_ACCOUNT" || !reflect.DeepEqual(input.Filter.And[2].Dimensions.Values, []string{"111111111111", "123456789012", "222222222222"}) || !reflect.DeepEqual(input.Filter.And[3].Dimensions.Values, []string{"us-east-1"}) {
					t.Fatalf("account, usage, or region scope is incorrect: %+v", input.Filter)
				}
				var groups []cetypes.Group
				switch input.Filter.And[1].Dimensions.Values[0] {
				case "Amazon Relational Database Service":
					groups = []cetypes.Group{supportCostGroup("111111111111", "15", "USD"), supportCostGroup("222222222222", "20", "USD"), supportCostGroup("123456789012", "3", "USD")}
				case "Amazon Elastic Kubernetes Service":
					groups = []cetypes.Group{supportCostGroup("111111111111", "4", "USD"), supportCostGroup("222222222222", "7", "USD")}
				case "Amazon ElastiCache":
					groups = []cetypes.Group{supportCostGroup("111111111111", "3", "USD")}
				default:
					t.Fatalf("unexpected service %v", input.Filter.And[1])
				}
				encoded, err := json.Marshal(struct{ ResultsByTime []cetypes.ResultByTime }{[]cetypes.ResultByTime{listCostTestBucket(groups...)}})
				if err != nil {
					t.Fatal(err)
				}
				body = string(encoded)
			default:
				t.Fatalf("unexpected billing operation %s", operation)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	}
	original := listCostInventory(
		listCostRecord("111111111111", ServiceRDS, "one"), listCostRecord("111111111111", ServiceRDS, "two"),
		listCostRecord("222222222222", ServiceEKS, "eks"), listCostRecord("123456789012", ServiceRDS, "payer-db"),
	)
	query := ListCostQuery{
		Now: listCostTestNow, Services: []Service{ServiceRDS, ServiceEKS, ServiceElastiCache, ServiceEKS},
		Regions: []string{"us-east-1", "us-west-2"}, SkipRegions: []string{"us-west-2"},
		Targets: []cost.AccountTarget{
			{AccountID: "111111111111", PayerAccountID: "123456789012", AWSConfig: config},
			{AccountID: "222222222222", PayerAccountID: "123456789012", AWSConfig: config},
			{AccountID: "123456789012", AWSConfig: config},
			{AccountID: "999999999999", PayerAccountID: "987654321098"}, // no current findings
		},
	}
	result, err := EnrichListCosts(context.Background(), query, original)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 4 || result.Summary.TotalCount != 4 || len(result.Summary.ByAccount) != 3 || len(result.Summary.ByService) != 3 || result.Summary.Costs.Start != "2026-10-10" || result.Summary.Costs.EndExclusive != "2026-11-09" || len(result.Summary.Costs.Warnings) != 0 {
		t.Fatalf("summary=%+v requests=%d", result.Summary, requests)
	}
	for _, row := range result.Summary.ByAccount {
		want := map[string]float64{"111111111111": 22, "222222222222": 27, "123456789012": 3}[row.AccountID]
		if row.SupportCost.Amount == nil || *row.SupportCost.Amount != want || row.SupportCost.Currency != "USD" {
			t.Fatalf("account=%+v", row)
		}
	}
	for _, row := range result.Summary.ByService {
		want := map[Service]float64{ServiceRDS: 38, ServiceEKS: 11, ServiceElastiCache: 3}[row.Service]
		if row.SupportCost.Amount == nil || *row.SupportCost.Amount != want || row.SupportCost.Currency != "USD" || (row.Service == ServiceElastiCache && row.Count != 0) {
			t.Fatalf("service=%+v", row)
		}
	}
	if original.Summary.ByAccount[0].SupportCost != nil || original.Summary.ByService[0].SupportCost != nil || !reflect.DeepEqual(result.Records, original.Records) {
		t.Fatal("enrichment changed original count summaries or resource details")
	}
}

func TestListCostsFailuresAndCurrenciesNeverProducePartialServiceTotals(t *testing.T) {
	for _, failure := range []string{"source denied", "mixed currency", "missing credentials"} {
		t.Run(failure, func(t *testing.T) {
			q := ListCostQuery{Now: listCostTestNow, Services: []Service{ServiceRDS}, Targets: []cost.AccountTarget{
				{AccountID: "111111111111", PayerAccountID: "123456789012"}, {AccountID: "222222222222", PayerAccountID: "987654321098"},
			}}
			if failure == "missing credentials" {
				q.Targets = q.Targets[:1]
			}
			q.newClient = func(target cost.AccountTarget) BillingAPI {
				return fakeBillingAPI{
					dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
						return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
					},
					usage: func(context.Context, *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
						currency, amount := "USD", "10"
						if target.AccountID == "222222222222" {
							if failure == "source denied" {
								return nil, errors.New("billing denied")
							}
							currency, amount = "EUR", "20"
						}
						return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{listCostTestBucket(supportCostGroup(target.AccountID, amount, currency))}}, nil
					},
				}
			}
			result, err := EnrichListCosts(context.Background(), q, listCostInventory(listCostRecord("111111111111", ServiceRDS, "one"), listCostRecord("222222222222", ServiceRDS, "two")))
			if err != nil || result.Summary.TotalCount != 2 || result.Summary.ByService[0].SupportCost.Amount != nil || len(result.Summary.Costs.Warnings) == 0 || *result.Summary.ByAccount[0].SupportCost.Amount != 10 {
				t.Fatalf("result=%+v err=%v", result.Summary, err)
			}
			if failure == "mixed currency" {
				if *result.Summary.ByAccount[1].SupportCost.Amount != 20 {
					t.Fatal(result.Summary.ByAccount)
				}
			} else if result.Summary.ByAccount[1].SupportCost.Amount != nil {
				t.Fatal("unavailable account became zero")
			}
		})
	}
}

func TestListCostsZeroNegativeEmptyAndExcludedScopes(t *testing.T) {
	for _, scope := range []string{"zero", "negative", "empty inventory", "excluded regions"} {
		t.Run(scope, func(t *testing.T) {
			calls := 0
			query := ListCostQuery{Now: listCostTestNow, Services: []Service{ServiceRDS}, Targets: []cost.AccountTarget{{AccountID: "111111111111"}}}
			inventory := listCostInventory(listCostRecord("111111111111", ServiceRDS, "db"))
			if scope == "empty inventory" {
				inventory = listCostInventory()
			}
			if scope == "excluded regions" {
				query.Regions, query.SkipRegions = []string{"us-east-1"}, []string{"us-east-1"}
			}
			query.newClient = func(cost.AccountTarget) BillingAPI {
				calls++
				return fakeBillingAPI{
					dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
						values := []string{"BoxUsage"}
						if scope == "negative" {
							values = append(values, "ExtendedSupport")
						}
						return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions(values...)}, nil
					},
					usage: func(context.Context, *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
						return &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{listCostTestBucket(supportCostGroup("111111111111", "-3", "USD"))}}, nil
					},
				}
			}
			result, err := EnrichListCosts(context.Background(), query, inventory)
			if err != nil {
				t.Fatal(err)
			}
			if scope == "excluded regions" || scope == "empty inventory" {
				if calls != 0 {
					t.Fatal("empty scope made a billing request")
				}
			}
			if scope != "empty inventory" {
				want := 0.0
				if scope == "negative" {
					want = -3
				}
				if result.Summary.ByAccount[0].SupportCost.Amount == nil || *result.Summary.ByAccount[0].SupportCost.Amount != want || *result.Summary.ByService[0].SupportCost.Amount != want {
					t.Fatalf("summary=%+v", result.Summary)
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := EnrichListCosts(ctx, ListCostQuery{}, Result{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
