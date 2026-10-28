package extendedsupport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/openshift-online/finops-tools/core/apilog"
)

type billingHTTPClient func(*http.Request) (*http.Response, error)

func (f billingHTTPClient) Do(request *http.Request) (*http.Response, error) { return f(request) }

func TestBillingDiscoveryUsesPayerSDKRequestsAndLogsBothAPIs(t *testing.T) {
	requests := 0
	var logs []string
	ctx := apilog.WithLog(context.Background(), func(line string) { logs = append(logs, line) })
	result, err := DiscoverBillingCandidates(ctx, aws.Config{
		Region: "us-west-2", Credentials: credentials.NewStaticCredentialsProvider("payer", "secret", "token"),
		HTTPClient: billingHTTPClient(func(request *http.Request) (*http.Response, error) {
			requests++
			if !strings.Contains(request.URL.Host, "us-east-1") {
				t.Errorf("Cost Explorer endpoint = %s", request.URL.Host)
			}
			var input struct {
				Dimension  string
				Context    string
				TimePeriod struct{ Start, End string }
				Filter     struct {
					Dimensions struct {
						Key          string
						Values       []string
						MatchOptions []string
					}
				}
				GroupBy []struct{ Type, Key string }
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if input.TimePeriod.Start != "2026-09-20" || input.TimePeriod.End != "2026-10-20" {
				t.Errorf("time period = %+v", input.TimePeriod)
			}
			var body string
			if strings.HasSuffix(request.Header.Get("X-Amz-Target"), "GetDimensionValues") {
				if input.Dimension != "USAGE_TYPE" || input.Context != "COST_AND_USAGE" {
					t.Errorf("dimensions = %+v", input)
				}
				body = `{"DimensionValues":[{"Value":"USE1-AmazonEKS-Hours:extendedSupport"},{"Value":"ordinary"}]}`
			} else {
				if !strings.HasSuffix(request.Header.Get("X-Amz-Target"), "GetCostAndUsage") || input.Filter.Dimensions.Key != "USAGE_TYPE" || len(input.Filter.Dimensions.Values) != 1 || input.Filter.Dimensions.Values[0] != "USE1-AmazonEKS-Hours:extendedSupport" || len(input.GroupBy) != 1 || input.GroupBy[0].Key != "LINKED_ACCOUNT" || input.GroupBy[0].Type != "DIMENSION" {
					t.Errorf("usage = %+v", input)
				}
				body = `{"ResultsByTime":[{"Groups":[{"Keys":["111111111111"],"Metrics":{"UnblendedCost":{"Amount":"0","Unit":"USD"}}}]}]}`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	}, billingTestPeriod())
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(result.AccountIDs) != 1 || result.AccountIDs[0] != "111111111111" || len(logs) != 2 || !strings.Contains(logs[0], "CostExplorer.GetDimensionValues") || !strings.Contains(logs[1], "groupBy=LINKED_ACCOUNT") || !strings.Contains(logs[1], "USAGE_TYPE=USE1-AmazonEKS-Hours:extendedSupport") {
		t.Fatalf("result=%+v requests=%d logs=%v", result, requests, logs)
	}
}
