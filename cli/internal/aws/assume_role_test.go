// assume_role_test.go tests STS AssumeRole session construction.
package aws

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

func TestAssumeRoleRequiresRoleARN(t *testing.T) {
	_, err := AssumeRole(context.Background(), ProfileSession{
		AccessKeyID: "A", SecretAccessKey: "S", SessionToken: "T",
	}, "", "sess")
	if err == nil {
		t.Fatal("expected error")
	}
}

type assumeRoleHTTPFunc func(*http.Request) (*http.Response, error)

func (f assumeRoleHTTPFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestRoleAssumerReusesConfiguredCredentialsAndTransport(t *testing.T) {
	calls := 0
	cfg := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("PAYER", "SK", "ST"), HTTPClient: assumeRoleHTTPFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if !strings.Contains(r.Header.Get("Authorization"), "PAYER/") {
			t.Error("payer credentials were not used")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "AssumeRole") || !strings.Contains(string(body), "MemberRole") {
			t.Errorf("unexpected request body %q", body)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><AssumeRoleResult><Credentials><AccessKeyId>MEMBER</AccessKeyId><SecretAccessKey>SK</SecretAccessKey><SessionToken>ST</SessionToken></Credentials></AssumeRoleResult></AssumeRoleResponse>`))}, nil
	})}
	assumer := NewRoleAssumer(cfg)
	for range 2 {
		session, err := assumer.AssumeRole(context.Background(), "arn:aws:iam::111111111111:role/MemberRole", "finops-test")
		if err != nil || session.AccessKeyID != "MEMBER" || session.Region != "us-east-1" {
			t.Fatalf("err=%v region=%q", err, session.Region)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestAssumeRoleRequiresPayerKeys(t *testing.T) {
	_, err := AssumeRole(context.Background(), ProfileSession{}, "arn:aws:iam::111:role/X", "sess")
	if err == nil {
		t.Fatal("expected error")
	}
}
