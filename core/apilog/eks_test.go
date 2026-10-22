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
	"github.com/aws/aws-sdk-go-v2/service/eks"
)

type eksHTTPClient func(*http.Request) (*http.Response, error)

func (f eksHTTPClient) Do(request *http.Request) (*http.Response, error) { return f(request) }

func TestEKSLoggingUsesSDKRequests(t *testing.T) {
	requests := 0
	client := eks.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "secret", "token"),
		HTTPClient: eksHTTPClient(func(request *http.Request) (*http.Response, error) {
			requests++
			body := "{}"
			switch request.URL.Path {
			case "/clusters":
				if request.URL.Query().Get("nextToken") != "page" {
					t.Error("cluster pagination token missing")
				}
				body = `{"clusters":["example"]}`
			case "/clusters/example":
				body = `{"cluster":{"name":"example","version":"1.31","upgradePolicy":{"supportType":"EXTENDED"}}}`
			case "/cluster-versions":
				if request.URL.Query().Get("includeAll") != "true" || request.URL.Query().Get("nextToken") != "page" {
					t.Errorf("query = %s", request.URL.RawQuery)
				}
				body = `{"clusterVersions":[{"clusterVersion":"1.31","versionStatus":"EXTENDED_SUPPORT","endOfStandardSupportDate":1700000000,"endOfExtendedSupportDate":1900000000}]}`
			default:
				t.Errorf("unexpected request: %s", request.URL)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	})
	var logs []string
	ctx := WithLog(context.Background(), func(line string) { logs = append(logs, line) })
	wrapped := WrapEKS(client)
	listed, err := wrapped.ListClusters(ctx, &eks.ListClustersInput{NextToken: aws.String("page")})
	if err != nil || len(listed.Clusters) != 1 {
		t.Fatalf("list = %+v, error = %v", listed, err)
	}
	cluster, err := wrapped.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String("example")})
	if err != nil || aws.ToString(cluster.Cluster.Version) != "1.31" {
		t.Fatalf("cluster = %+v, error = %v", cluster, err)
	}
	versions, err := wrapped.DescribeClusterVersions(ctx, &eks.DescribeClusterVersionsInput{IncludeAll: aws.Bool(true), NextToken: aws.String("page")})
	if err != nil || len(versions.ClusterVersions) != 1 || versions.ClusterVersions[0].EndOfStandardSupportDate == nil {
		t.Fatalf("versions = %+v, error = %v", versions, err)
	}
	want := []string{"EKS.ListClusters page=next", "EKS.DescribeCluster name=example", "EKS.DescribeClusterVersions page=next"}
	if requests != 3 || !reflect.DeepEqual(logs, want) {
		t.Fatalf("requests=%d logs=%v", requests, logs)
	}
	if WrapEKS(nil) != nil {
		t.Fatal("nil client was wrapped")
	}
}
