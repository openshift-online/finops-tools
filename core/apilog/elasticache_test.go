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
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
)

func TestElastiCacheLoggingUsesSDKRequests(t *testing.T) {
	requests := 0
	arn := "arn:aws:elasticache:us-east-1:111111111111:cluster:cache"
	client := elasticache.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "secret", "token"),
		HTTPClient: eksHTTPClient(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.URL.Host != "elasticache.us-east-1.amazonaws.com" {
				t.Error(request.URL)
			}
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			operation := request.Form.Get("Action")
			var body string
			switch operation {
			case "DescribeCacheClusters":
				if request.Form.Get("Marker") != "page" || request.Form.Get("ShowCacheClustersNotInReplicationGroups") == "true" {
					t.Fatal(request.Form)
				}
				body = "<CacheClusters><CacheCluster><CacheClusterId>cache</CacheClusterId><Engine>redis</Engine><EngineVersion>5.0.6</EngineVersion></CacheCluster></CacheClusters><Marker>next</Marker>"
			case "DescribeServerlessCaches":
				if request.Form.Get("NextToken") != "page" {
					t.Fatal(request.Form)
				}
				body = "<ServerlessCaches><member><ServerlessCacheName>serverless</ServerlessCacheName><Engine>redis</Engine><MajorEngineVersion>7</MajorEngineVersion><FullEngineVersion>7.1</FullEngineVersion></member></ServerlessCaches><NextToken>next</NextToken>"
			case "ListTagsForResource":
				if request.Form.Get("ResourceName") != arn {
					t.Fatal(request.Form)
				}
				body = "<TagList><Tag><Key>team</Key><Value>finops</Value></Tag></TagList>"
			default:
				t.Fatalf("unexpected operation: %s", operation)
			}
			body = "<" + operation + "Response xmlns=\"http://elasticache.amazonaws.com/doc/2015-02-02/\"><" + operation + "Result>" + body + "</" + operation + "Result></" + operation + "Response>"
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/xml"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	})
	var logs []string
	ctx := WithLog(context.Background(), func(line string) { logs = append(logs, line) })
	wrapped := WrapElastiCache(client)
	clusters, err := wrapped.DescribeCacheClusters(ctx, &elasticache.DescribeCacheClustersInput{Marker: aws.String("page")})
	if err != nil || clusters == nil || len(clusters.CacheClusters) != 1 || aws.ToString(clusters.CacheClusters[0].EngineVersion) != "5.0.6" || aws.ToString(clusters.Marker) != "next" {
		t.Fatalf("clusters=%+v err=%v", clusters, err)
	}
	serverless, err := wrapped.DescribeServerlessCaches(ctx, &elasticache.DescribeServerlessCachesInput{NextToken: aws.String("page")})
	if err != nil || serverless == nil || len(serverless.ServerlessCaches) != 1 || aws.ToString(serverless.ServerlessCaches[0].FullEngineVersion) != "7.1" || aws.ToString(serverless.NextToken) != "next" {
		t.Fatalf("serverless=%+v err=%v", serverless, err)
	}
	tags, err := wrapped.ListTagsForResource(ctx, &elasticache.ListTagsForResourceInput{ResourceName: aws.String(arn)})
	if err != nil || tags == nil || len(tags.TagList) != 1 || aws.ToString(tags.TagList[0].Value) != "finops" {
		t.Fatalf("tags=%+v err=%v", tags, err)
	}
	want := []string{"ElastiCache.DescribeCacheClusters page=next", "ElastiCache.DescribeServerlessCaches page=next", "ElastiCache.ListTagsForResource resource=" + arn}
	if requests != 3 || !reflect.DeepEqual(logs, want) {
		t.Fatalf("requests=%d logs=%v", requests, logs)
	}
	if WrapElastiCache(nil) != nil {
		t.Fatal("nil client wrapped")
	}
}
