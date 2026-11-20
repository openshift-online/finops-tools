package awshealth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/health"
	ht "github.com/aws/aws-sdk-go-v2/service/health/types"
	"github.com/openshift-online/finops-tools/core/apilog"
)

func TestCollectorsRejectIncompletePagesAndPaginationCycles(t *testing.T) {
	for _, failure := range []string{"cycle", "nil", "denied"} {
		t.Run(failure, func(t *testing.T) {
			api := fakeAPI{
				events: func(*health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
					if failure == "nil" {
						return nil, nil
					}
					if failure == "denied" {
						return nil, errors.New("denied")
					}
					return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{testEvent("event", "PLANNED_LIFECYCLE_EVENT", "RDS", 1)}, NextToken: aws.String("repeated")}, nil
				},
				accounts: func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
					if failure == "nil" {
						return nil, nil
					}
					if failure == "denied" {
						return nil, errors.New("denied")
					}
					return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{testAccount}, NextToken: aws.String("repeated")}, nil
				},
				resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
					if failure == "nil" {
						return nil, nil
					}
					if failure == "denied" {
						return nil, errors.New("denied")
					}
					out, _ := fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
					out.NextToken = aws.String("repeated")
					return out, nil
				},
			}
			client := NewClientWithAPI(api)
			if events, err := client.ListUpcomingEvents(context.Background(), testNow); err == nil || events != nil {
				t.Fatal(events, err)
			}
			if accounts, err := client.AffectedAccounts(context.Background(), "event"); err == nil || accounts != nil {
				t.Fatal(accounts, err)
			}
			if resources, err := client.AffectedResources(context.Background(), "event", testAccount); err == nil || resources != nil {
				t.Fatal(resources, err)
			}
			if result, err := FetchUpcoming(context.Background(), testQuery(api)); err == nil || len(result.Events) != 0 {
				t.Fatal(result, err)
			}
		})
	}
}

func TestCollectorsRejectResponsesOutsideRequestedScope(t *testing.T) {
	api := fakeAPI{
		details: func(in *health.DescribeEventDetailsForOrganizationInput) (*health.DescribeEventDetailsForOrganizationOutput, error) {
			out := detailResponse(in, "Upgrade")
			out.SuccessfulSet[0].AwsAccountId = aws.String("222222222222")
			return out, nil
		},
		resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
			out, _ := fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
			out.Entities[0].EventArn = aws.String("other-event")
			return out, nil
		},
		accounts: func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
			return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{"invalid"}}, nil
		},
	}
	client := NewClientWithAPI(api)
	if _, err := client.EventDetails(context.Background(), "event", testAccount); err == nil {
		t.Fatal("wrong account accepted")
	}
	if _, err := client.AffectedResources(context.Background(), "event", testAccount); err == nil {
		t.Fatal("wrong event accepted")
	}
	if _, err := client.AffectedAccounts(context.Background(), "event"); err == nil {
		t.Fatal("invalid account accepted")
	}
}

type healthHTTPClient func(*http.Request) (*http.Response, error)

func (f healthHTTPClient) Do(request *http.Request) (*http.Response, error) { return f(request) }

func TestUpcomingUsesOrganizationalSDKAPIsAndLogsWithoutTokens(t *testing.T) {
	var calls, logs []string
	client := NewClient(aws.Config{Region: "us-west-2", Credentials: credentials.NewStaticCredentialsProvider("test", "secret", "token"),
		HTTPClient: healthHTTPClient(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host != "health.us-east-1.amazonaws.com" {
				t.Fatal(request.URL)
			}
			op := strings.TrimPrefix(request.Header.Get("X-Amz-Target"), "AWSHealth_20160804.")
			calls = append(calls, op)
			var input map[string]json.RawMessage
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			body := ""
			switch op {
			case "DescribeEventsForOrganization":
				body = `{"events":[{"arn":"event","eventTypeCode":"AWS_RDS_PLANNED_LIFECYCLE_EVENT","eventTypeCategory":"scheduledChange","service":"RDS","region":"us-east-1","startTime":2000000000,"statusCode":"upcoming","eventScopeCode":"ACCOUNT_SPECIFIC"}]}`
			case "DescribeAffectedAccountsForOrganization":
				body = `{"affectedAccounts":["111111111111"],"eventScopeCode":"ACCOUNT_SPECIFIC"}`
			case "DescribeEventDetailsForOrganization":
				if !strings.Contains(string(input["organizationEventDetailFilters"]), testAccount) {
					t.Fatal(input)
				}
				body = `{"successfulSet":[{"awsAccountId":"111111111111","event":{"arn":"event"},"eventDescription":{"latestDescription":"Upgrade before Extended Support begins"}}]}`
			case "DescribeAffectedEntitiesForOrganization":
				if _, ok := input["organizationEntityFilters"]; ok {
					t.Fatal("deprecated entity filter")
				}
				if !strings.Contains(string(input["organizationEntityAccountFilters"]), testAccount) {
					t.Fatal(input)
				}
				body = `{"entities":[{"awsAccountId":"111111111111","eventArn":"event","entityValue":"arn:aws:rds:us-east-1:111111111111:db:production","entityArn":"arn:aws:health:us-east-1:111111111111:entity/123","statusCode":"PENDING"}]}`
			default:
				t.Fatalf("unexpected operation %s", op)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	}, func(o *ClientOptions) { o.RequestInterval = time.Millisecond })
	q := testQuery(fakeAPI{})
	q.newClient = func(aws.Config) *Client { return client }
	ctx := apilog.WithLog(context.Background(), func(line string) { logs = append(logs, line) })
	result, err := FetchUpcoming(ctx, q)
	if err != nil || !result.Complete || len(result.Events) != 1 || len(result.Events[0].Accounts[0].Resources) != 1 || len(calls) != 4 || len(logs) != 4 {
		t.Fatal(result, calls, logs, err)
	}
}
