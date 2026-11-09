package extendedsupport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/openshift-online/finops-tools/core/apilog"
	"github.com/openshift-online/finops-tools/core/cost"
)

func TestSupportCostsSDKUsesPayerBillingAndHistoricalServiceNames(t *testing.T) {
	requests := 0
	var logs []string
	ctx := apilog.WithLog(context.Background(), func(line string) { logs = append(logs, line) })
	period, err := cost.ResolvePeriod(cost.PeriodSpec{Days: 90, ExcludeRecentDays: 2}, time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	q := CostQuery{Range: period, Services: []Service{ServiceEKS}, Regions: []string{"us-east-1", "us-west-2"}, SkipRegions: []string{"us-west-2"}}
	q.Targets = []cost.AccountTarget{{AccountID: "111111111111", PayerAccountID: "123456789012", AWSConfig: aws.Config{
		Region: "us-west-2", Credentials: credentials.NewStaticCredentialsProvider("payer", "secret", "token"),
		HTTPClient: billingHTTPClient(func(request *http.Request) (*http.Response, error) {
			requests++
			if !strings.Contains(request.URL.Host, "us-east-1") {
				t.Errorf("wrong billing endpoint: %s", request.URL.Host)
			}
			var input struct {
				TimePeriod  struct{ Start, End string }
				Granularity string
				Metrics     []string
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
			if input.TimePeriod.Start != "2026-08-04" || input.TimePeriod.End != "2026-11-02" {
				t.Fatalf("billing window=%+v", input.TimePeriod)
			}
			body := `{"DimensionValues":[{"Value":"USE1-AmazonEKS-Hours:extendedSupport"}]}`
			if strings.HasSuffix(request.Header.Get("X-Amz-Target"), "GetCostAndUsage") {
				if input.Granularity != "MONTHLY" || !reflect.DeepEqual(input.Metrics, []string{"NetUnblendedCost"}) || len(input.Filter.And) != 4 {
					t.Fatalf("request=%+v", input)
				}
				if !reflect.DeepEqual(input.Filter.And[1].Dimensions.Values, []string{"Amazon Elastic Kubernetes Service", "Amazon Elastic Container Service for Kubernetes"}) || !reflect.DeepEqual(input.Filter.And[2].Dimensions.Values, []string{"111111111111"}) {
					t.Fatalf("filter=%+v", input.Filter)
				}
				if input.Filter.And[3].Dimensions.Key != "REGION" || !reflect.DeepEqual(input.Filter.And[3].Dimensions.Values, []string{"us-east-1"}) {
					t.Fatalf("billing region filter=%+v", input.Filter.And[3])
				}
				body = `{"ResultsByTime":[{"TimePeriod":{"Start":"2026-11-01","End":"2026-11-02"},"Estimated":true,"Groups":[{"Keys":["111111111111"],"Metrics":{"NetUnblendedCost":{"Amount":"14.4","Unit":"USD"}}}]}]}`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	}}}
	r, err := FetchCosts(ctx, q)
	if err != nil || requests != 2 || len(logs) != 2 || r.Amount != 14.4 || r.AvoidableAmount != nil || !r.Estimated {
		t.Fatalf("result=%+v requests=%d logs=%v err=%v", r, requests, logs, err)
	}
	if r.StartDate != "2026-08-04" || r.EndDate != "2026-11-01" || r.Granularity != "monthly" || len(r.Rows) != 1 || r.Rows[0].EndDate != "2026-11-01" {
		t.Fatalf("buffered-period result=%+v", r)
	}
}
