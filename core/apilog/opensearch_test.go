package apilog

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
)

func TestOpenSearchLoggingUsesSDKRequests(t *testing.T) {
	requests := 0
	arn := "arn:aws:es:us-east-1:111111111111:domain/old"
	client := opensearch.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "secret", "token"),
		HTTPClient: eksHTTPClient(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.URL.Host != "es.us-east-1.amazonaws.com" || request.Method != http.MethodGet {
				t.Fatal(request.URL, request.Method)
			}
			var body string
			switch request.URL.Path {
			case "/2021-01-01/domain":
				if request.URL.Query().Get("engineType") != "" {
					t.Fatal("legacy Elasticsearch excluded")
				}
				body = `{"DomainNames":[{"DomainName":"old","EngineType":"Elasticsearch"},{"DomainName":"modern","EngineType":"OpenSearch"}]}`
			case "/2021-01-01/opensearch/domain/old":
				body = `{"DomainStatus":{"DomainName":"old","DomainId":"111111111111/old","ARN":"` + arn + `","EngineVersion":"Elasticsearch_7.8","Created":true,"Deleted":false}}`
			case "/2021-01-01/tags":
				if request.URL.Query().Get("arn") != arn {
					t.Fatal(request.URL)
				}
				body = `{"TagList":[{"Key":"team","Value":"finops"}]}`
			default:
				t.Fatalf("unexpected request: %s", request.URL)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	})
	var logs []string
	ctx := WithLog(context.Background(), func(line string) { logs = append(logs, line) })
	wrapped := WrapOpenSearch(client)
	names, err := wrapped.ListDomainNames(ctx, &opensearch.ListDomainNamesInput{})
	if err != nil || names == nil || len(names.DomainNames) != 2 || names.DomainNames[0].EngineType != "Elasticsearch" {
		t.Fatalf("names=%+v err=%v", names, err)
	}
	described, err := wrapped.DescribeDomain(ctx, &opensearch.DescribeDomainInput{DomainName: aws.String("old")})
	if err != nil || described == nil || described.DomainStatus == nil || aws.ToString(described.DomainStatus.EngineVersion) != "Elasticsearch_7.8" || aws.ToString(described.DomainStatus.ARN) != arn {
		t.Fatalf("domain=%+v err=%v", described, err)
	}
	tags, err := wrapped.ListTags(ctx, &opensearch.ListTagsInput{ARN: aws.String(arn)})
	if err != nil || tags == nil || len(tags.TagList) != 1 || aws.ToString(tags.TagList[0].Value) != "finops" {
		t.Fatalf("tags=%+v err=%v", tags, err)
	}
	want := []string{"OpenSearch.ListDomainNames", "OpenSearch.DescribeDomain name=old", "OpenSearch.ListTags resource=" + arn}
	if requests != 3 || !reflect.DeepEqual(logs, want) {
		t.Fatalf("requests=%d logs=%v", requests, logs)
	}
	if WrapOpenSearch(nil) != nil {
		t.Fatal("nil client wrapped")
	}
}
