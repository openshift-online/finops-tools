package apilog

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
	"github.com/aws/aws-sdk-go-v2/service/health"
)

func TestHealthLoggingUsesSDKRequests(t *testing.T) {
	requests := 0
	client := health.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "secret", "token"),
		HTTPClient: eksHTTPClient(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.URL.Host != "health.us-east-1.amazonaws.com" || request.Method != http.MethodPost || request.Header.Get("X-Amz-Target") != "AWSHealth_20160804.DescribeServiceLifecycle" {
				t.Fatal(request.URL, request.Method, request.Header)
			}
			var input struct {
				MaxResults int32  `json:"maxResults"`
				NextToken  string `json:"nextToken"`
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.MaxResults != 20 || input.NextToken != "opaque-private-token" {
				t.Fatal(input)
			}
			body := `{"serviceLifecycles":[{"service":"ELASTICACHE","version":"redis/6","lifecycleEvents":[{"lifecycleEventType":"END_OF_STANDARD_SUPPORT","date":1900000000,"regions":["all"],"impactRisks":["BILLING"]},{"lifecycleEventType":"END_OF_EXTENDED_SUPPORT","date":2000000000,"regions":["all"]}]}],"nextToken":"next"}`
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	})
	var logs []string
	ctx := WithLog(context.Background(), func(line string) { logs = append(logs, line) })
	wrapped := WrapHealth(client)
	out, err := wrapped.DescribeServiceLifecycle(ctx, &health.DescribeServiceLifecycleInput{MaxResults: aws.Int32(20), NextToken: aws.String("opaque-private-token")})
	if err != nil || out == nil || len(out.ServiceLifecycles) != 1 || len(out.ServiceLifecycles[0].LifecycleEvents) != 2 || aws.ToString(out.NextToken) != "next" {
		t.Fatalf("output=%+v err=%v", out, err)
	}
	event := out.ServiceLifecycles[0].LifecycleEvents[0]
	if !event.Date.Equal(time.Unix(1900000000, 0)) || event.Regions[0] != "all" || event.ImpactRisks[0] != "BILLING" {
		t.Fatal(event)
	}
	if requests != 1 || !reflect.DeepEqual(logs, []string{"Health.DescribeServiceLifecycle page=next"}) {
		t.Fatal(requests, logs)
	}
	if WrapHealth(nil) != nil {
		t.Fatal("nil client wrapped")
	}
}
