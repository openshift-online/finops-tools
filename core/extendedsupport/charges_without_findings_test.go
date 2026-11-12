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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/cost"
)

func TestAccountsWithoutFindingsExcludesCurrentAndSkippedAccounts(t *testing.T) {
	targets := []cost.AccountTarget{{AccountID: "111111111111"}, {AccountID: "222222222222"}, {AccountID: "333333333333"}, {AccountID: "333333333333"}, {AccountID: "444444444444"}}
	result := listCostInventory(listCostRecord("111111111111", ServiceRDS, "current"))
	result.Summary.SkippedAccounts = []AccountWarning{{AccountID: "222222222222"}}
	result.Summary.UnclassifiedResources = []ResourceWarning{{AccountID: "333333333333"}}
	result.Summary.PastExtendedSupportResources = []Record{listCostRecord("444444444444", ServiceRDS, "expired")}
	got := AccountsWithoutFindings(targets, result)
	if len(got) != 2 || got[0].AccountID != "333333333333" || got[1].AccountID != "444444444444" {
		t.Fatalf("candidates=%+v", got)
	}
}

func TestChargesWithoutFindingsSDKUsesFiveDaysScopedAccountsAndOnlyPositiveTotals(t *testing.T) {
	requests := 0
	config := aws.Config{Region: "us-west-2", Credentials: credentials.NewStaticCredentialsProvider("payer", "secret", "token"), HTTPClient: billingHTTPClient(func(request *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(request.URL.Host, "us-east-1") || !strings.Contains(request.Header.Get("Authorization"), "Credential=payer/") {
			t.Fatal("wrong billing endpoint or credentials")
		}
		var input struct {
			TimePeriod  struct{ Start, End string }
			Granularity string
			Metrics     []string
			GroupBy     []struct{ Key string }
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
		if input.TimePeriod.Start != "2026-11-04" || input.TimePeriod.End != "2026-11-09" {
			t.Fatalf("five-day window=%+v", input.TimePeriod)
		}
		body := `{"DimensionValues":[{"Value":"BoxUsage"},{"Value":"USE1-ExtendedSupport"},{"Value":"USE1-UnimplementedService-ExtendedSupport"}]}`
		switch request.Header.Get("X-Amz-Target") {
		case "AWSInsightsIndexService.GetDimensionValues":
		case "AWSInsightsIndexService.GetCostAndUsage":
			if input.Granularity != "DAILY" || !reflect.DeepEqual(input.Metrics, []string{"NetUnblendedCost"}) || len(input.GroupBy) != 1 || input.GroupBy[0].Key != "LINKED_ACCOUNT" {
				t.Fatalf("request=%+v", input)
			}
			if len(input.Filter.And) != 3 || input.Filter.And[0].Dimensions.Key != "USAGE_TYPE" || !reflect.DeepEqual(input.Filter.And[0].Dimensions.Values, []string{"USE1-ExtendedSupport", "USE1-UnimplementedService-ExtendedSupport"}) || input.Filter.And[1].Dimensions.Key != "LINKED_ACCOUNT" || !reflect.DeepEqual(input.Filter.And[1].Dimensions.Values, []string{"111111111111", "123456789012", "222222222222", "333333333333"}) || input.Filter.And[2].Dimensions.Key != "REGION" || !reflect.DeepEqual(input.Filter.And[2].Dimensions.Values, []string{"us-east-1"}) {
				t.Fatalf("filter=%+v", input.Filter)
			}
			// Includes billing usage for an unimplemented service, even when only
			// RDS was selected for resource discovery. No SERVICE predicate exists.
			groups := []cetypes.Group{supportCostGroup("111111111111", "6", "USD"), supportCostGroup("222222222222", "0", "USD"), supportCostGroup("333333333333", "-2", "USD"), supportCostGroup("123456789012", "3", "USD")}
			encoded, err := json.Marshal(struct{ ResultsByTime []cetypes.ResultByTime }{[]cetypes.ResultByTime{listCostTestBucket(groups...)}})
			if err != nil {
				t.Fatal(err)
			}
			body = string(encoded)
		default:
			t.Fatal("unexpected billing API", request.Header.Get("X-Amz-Target"))
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	query := ListCostQuery{Now: listCostTestNow, Services: []Service{ServiceRDS}, Regions: []string{"us-east-1", "us-west-2"}, SkipRegions: []string{"us-west-2"}}
	for _, id := range []string{"111111111111", "222222222222", "333333333333", "123456789012", "444444444444", "555555555555"} {
		query.Targets = append(query.Targets, cost.AccountTarget{AccountID: id, PayerAccountID: "123456789012", DisplayAlias: "alias-" + id, AWSConfig: config})
	}
	result := listCostInventory(listCostRecord("444444444444", ServiceRDS, "known"))
	result.Summary.SkippedAccounts = []AccountWarning{{AccountID: "555555555555"}}
	result.Summary.UnclassifiedResources = []ResourceWarning{{AccountID: "111111111111"}}
	checked, err := FindChargesWithoutFindings(context.Background(), query, result)
	if err != nil {
		t.Fatal(err)
	}
	check := checked.Summary.ChargesWithoutFindings
	if requests != 2 || len(check.Accounts) != 2 || len(check.AccountIDs) != 4 || check.Start != "2026-11-04" || check.EndExclusive != "2026-11-09" || checked.Summary.TotalCount != 1 || len(checked.Records) != 1 || len(check.Warnings) != 0 || !check.AllServices || len(check.Services) != 0 {
		t.Fatalf("check=%+v requests=%d", check, requests)
	}
	if check.Accounts[0].AccountID != "111111111111" || check.Accounts[0].Amount != 6 || !check.Accounts[0].ScanIncomplete || check.Accounts[1].AccountID != "123456789012" || check.Accounts[1].Amount != 3 || check.Accounts[1].ScanIncomplete {
		t.Fatal(check.Accounts)
	}
	if check.Accounts[0].DisplayAlias != "alias-111111111111" || check.Accounts[0].Currency != "USD" || !reflect.DeepEqual(checked.Records, result.Records) || checked.Summary.Costs != nil {
		t.Fatal("five-day check changed current inventory or normal cost summaries")
	}
}

func TestChargesWithoutFindingsFailureIsolationAndNoPartialResults(t *testing.T) {
	q := ListCostQuery{Now: listCostTestNow, Services: []Service{ServiceRDS}, Targets: []cost.AccountTarget{{AccountID: "111111111111", PayerAccountID: "123456789012"}, {AccountID: "222222222222", PayerAccountID: "987654321098"}}}
	q.newClient = func(target cost.AccountTarget) BillingAPI {
		return fakeBillingAPI{
			dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
				return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("ExtendedSupport")}, nil
			},
			usage: func(_ context.Context, in *costexplorer.GetCostAndUsageInput) (*costexplorer.GetCostAndUsageOutput, error) {
				if target.AccountID == "222222222222" && in.NextPageToken != nil {
					return nil, errors.New("second billing page denied")
				}
				out := &costexplorer.GetCostAndUsageOutput{ResultsByTime: []cetypes.ResultByTime{listCostTestBucket(supportCostGroup(target.AccountID, "5", "USD"))}}
				if target.AccountID == "222222222222" {
					out.NextPageToken = aws.String("next")
				}
				return out, nil
			},
		}
	}
	result, err := FindChargesWithoutFindings(context.Background(), q, Result{})
	if err != nil || len(result.Summary.ChargesWithoutFindings.Accounts) != 1 || result.Summary.ChargesWithoutFindings.Accounts[0].AccountID != "111111111111" || len(result.Summary.ChargesWithoutFindings.Warnings) != 1 || !strings.Contains(result.Summary.ChargesWithoutFindings.Warnings[0], "second billing page denied") {
		t.Fatalf("result=%+v err=%v", result.Summary.ChargesWithoutFindings, err)
	}
}

func TestChargesWithoutFindingsEmptyAndExcludedScopesAndCancellation(t *testing.T) {
	for _, scope := range []string{"no targets", "current findings", "skipped account", "excluded regions", "no charges"} {
		t.Run(scope, func(t *testing.T) {
			calls := 0
			q := ListCostQuery{Now: listCostTestNow, Services: []Service{ServiceRDS}, Targets: []cost.AccountTarget{{AccountID: "111111111111"}}}
			result := Result{}
			switch scope {
			case "no targets":
				q.Targets = nil
			case "current findings":
				result = listCostInventory(listCostRecord("111111111111", ServiceRDS, "known"))
			case "skipped account":
				result.Summary.SkippedAccounts = []AccountWarning{{AccountID: "111111111111"}}
			case "excluded regions":
				q.Regions, q.SkipRegions = []string{"us-east-1"}, []string{"us-east-1"}
			}
			q.newClient = func(cost.AccountTarget) BillingAPI {
				calls++
				return fakeBillingAPI{dimensions: func(context.Context, *costexplorer.GetDimensionValuesInput) (*costexplorer.GetDimensionValuesOutput, error) {
					return &costexplorer.GetDimensionValuesOutput{DimensionValues: billingDimensions("BoxUsage")}, nil
				}}
			}
			got, err := FindChargesWithoutFindings(context.Background(), q, result)
			wantCalls := 0
			if scope == "no charges" {
				wantCalls = 1
			}
			if err != nil || calls != wantCalls || len(got.Summary.ChargesWithoutFindings.Accounts) != 0 || len(got.Summary.ChargesWithoutFindings.Warnings) != 0 {
				t.Fatalf("result=%+v calls=%d err=%v", got.Summary.ChargesWithoutFindings, calls, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FindChargesWithoutFindings(ctx, ListCostQuery{}, Result{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
