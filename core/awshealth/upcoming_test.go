package awshealth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/health"
	ht "github.com/aws/aws-sdk-go-v2/service/health/types"
)

var testNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

const testAccount = "111111111111"

type fakeAPI struct {
	events    func(*health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error)
	accounts  func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error)
	details   func(*health.DescribeEventDetailsForOrganizationInput) (*health.DescribeEventDetailsForOrganizationOutput, error)
	resources func(*health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error)
}

func (f fakeAPI) DescribeEventsForOrganization(_ context.Context, in *health.DescribeEventsForOrganizationInput, _ ...func(*health.Options)) (*health.DescribeEventsForOrganizationOutput, error) {
	if f.events != nil {
		return f.events(in)
	}
	return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{testEvent("event", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 1)}}, nil
}

func (f fakeAPI) DescribeAffectedAccountsForOrganization(_ context.Context, in *health.DescribeAffectedAccountsForOrganizationInput, _ ...func(*health.Options)) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
	if f.accounts != nil {
		return f.accounts(in)
	}
	return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{testAccount}, EventScopeCode: ht.EventScopeCodeAccountSpecific}, nil
}

func (f fakeAPI) DescribeEventDetailsForOrganization(_ context.Context, in *health.DescribeEventDetailsForOrganizationInput, _ ...func(*health.Options)) (*health.DescribeEventDetailsForOrganizationOutput, error) {
	if f.details != nil {
		return f.details(in)
	}
	return detailResponse(in, "Upgrade the resource."), nil
}

func detailResponse(in *health.DescribeEventDetailsForOrganizationInput, description string) *health.DescribeEventDetailsForOrganizationOutput {
	filter := in.OrganizationEventDetailFilters[0]
	return &health.DescribeEventDetailsForOrganizationOutput{SuccessfulSet: []ht.OrganizationEventDetails{{
		AwsAccountId: filter.AwsAccountId, Event: &ht.Event{Arn: filter.EventArn},
		EventDescription: &ht.EventDescription{LatestDescription: aws.String(description)},
		EventMetadata:    map[string]string{"guidance": "upgrade"},
	}}}
}

func (f fakeAPI) DescribeAffectedEntitiesForOrganization(_ context.Context, in *health.DescribeAffectedEntitiesForOrganizationInput, _ ...func(*health.Options)) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
	if f.resources != nil {
		return f.resources(in)
	}
	filter := in.OrganizationEntityAccountFilters[0]
	return &health.DescribeAffectedEntitiesForOrganizationOutput{Entities: []ht.AffectedEntity{{
		AwsAccountId: filter.AwsAccountId, EventArn: filter.EventArn, EntityValue: aws.String("db-production"),
		EntityArn:  aws.String("arn:aws:health:us-east-1:111111111111:entity/123"),
		StatusCode: ht.EntityStatusCodeResolved, LastUpdatedTime: &testNow, EntityMetadata: map[string]string{"engine": "mysql"},
	}}}, nil
}

func testEvent(arn, code, service string, days int) ht.OrganizationEvent {
	start := testNow.AddDate(0, 0, days)
	return ht.OrganizationEvent{Arn: aws.String(arn), EventTypeCode: aws.String(code), Service: aws.String(service), Region: aws.String("us-east-1"), StartTime: &start, StatusCode: ht.EventStatusCodeUpcoming, EventTypeCategory: ht.EventTypeCategoryScheduledChange, EventScopeCode: ht.EventScopeCodeAccountSpecific}
}

func testQuery(api fakeAPI) Query {
	return Query{Targets: []Target{{AccountID: "123456789012"}}, Now: testNow, Workers: 1,
		newClient: func(aws.Config) *Client { return NewClientWithAPI(api) }}
}

func TestUpcomingCollectsAllServicesMatchesDescriptionsAndSortsAcrossPayers(t *testing.T) {
	pages, resourceCalls := 0, 0
	api := fakeAPI{
		events: func(in *health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
			pages++
			if !in.Filter.StartTime.From.Equal(testNow) || len(in.Filter.EventTypeCategories) != 1 || in.Filter.EventTypeCategories[0] != ht.EventTypeCategoryScheduledChange || !reflect.DeepEqual(in.Filter.EventStatusCodes, []ht.EventStatusCode{ht.EventStatusCodeUpcoming, ht.EventStatusCodeOpen}) {
				t.Fatalf("filter=%+v", in.Filter)
			}
			if in.NextToken == nil {
				old := testEvent("old", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", -1)
				closed := testEvent("closed", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 1)
				closed.StatusCode = ht.EventStatusCodeClosed
				return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{
					testEvent("late", "aws_rds_planned_lifecycle_event", "RDS", 8), old, closed,
				}, NextToken: aws.String("page-two")}, nil
			}
			return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{
				testEvent("early", "AWS_LAMBDA_UPGRADE", "LAMBDA", 1),
				testEvent("late", "aws_rds_planned_lifecycle_event", "RDS", 8),
				testEvent("maintenance", "AWS_EC2_MAINTENANCE", "EC2", 2),
			}}, nil
		},
		details: func(in *health.DescribeEventDetailsForOrganizationInput) (*health.DescribeEventDetailsForOrganizationOutput, error) {
			text := "Ordinary maintenance"
			if aws.ToString(in.OrganizationEventDetailFilters[0].EventArn) == "early" {
				text = "A Planned\n Lifecycle Event requires an upgrade."
			}
			return detailResponse(in, text), nil
		},
		resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
			resourceCalls++
			if len(in.OrganizationEntityAccountFilters[0].StatusCodes) != 0 {
				t.Fatal("status restriction used")
			}
			return fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
		},
	}
	q := testQuery(api)
	q.Targets = append(q.Targets, Target{AccountID: "987654321098"}, q.Targets[0])
	result, err := FetchUpcoming(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || len(result.Events) != 4 || pages != 4 || resourceCalls != 4 || len(result.PayerAccountIDs) != 2 {
		t.Fatalf("result=%+v pages=%d resources=%d", result, pages, resourceCalls)
	}
	if result.Events[0].ARN != "early" || result.Events[1].ARN != "early" || result.Events[2].ARN != "late" {
		t.Fatal(result.Events)
	}
	account := result.Events[0].Accounts[0]
	resource := account.Resources[0]
	if !account.ResourcesComplete || !result.Events[0].AccountsComplete || account.Metadata["guidance"] != "upgrade" || resource.Status != "RESOLVED" || resource.ResourceID != "db-production" || !strings.Contains(resource.EntityARN, ":health:") || resource.Metadata["engine"] != "mysql" || !resource.LastUpdatedTime.Equal(testNow) {
		t.Fatalf("account=%+v resource=%+v", account, resource)
	}
}

func TestReusableAccountAndResourceCollectorsPaginateAndDeduplicate(t *testing.T) {
	api := fakeAPI{
		accounts: func(in *health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
			if aws.ToString(in.EventArn) != "event" {
				t.Fatal(in)
			}
			if in.NextToken == nil {
				return &health.DescribeAffectedAccountsForOrganizationOutput{NextToken: aws.String("more")}, nil
			}
			return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{"222222222222", testAccount, testAccount}}, nil
		},
		resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
			out, _ := fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
			if in.NextToken == nil {
				out.NextToken = aws.String("more")
			} else {
				other := out.Entities[0]
				other.EntityArn, other.EntityValue, other.StatusCode = aws.String("health-entity-2"), aws.String("arn:aws:rds:us-east-1:111111111111:db:other"), ""
				out.Entities = append(out.Entities, other)
			}
			return out, nil
		},
	}
	client := NewClientWithAPI(api)
	accounts, err := client.AffectedAccounts(context.Background(), "event")
	if err != nil || !reflect.DeepEqual(accounts, []string{testAccount, "222222222222"}) {
		t.Fatal(accounts, err)
	}
	resources, err := client.AffectedResources(context.Background(), "event", testAccount)
	if err != nil || len(resources) != 2 || resources[0].Status != "" || !strings.HasPrefix(resources[0].ResourceID, "arn:aws:rds:") {
		t.Fatal(resources, err)
	}
}

func TestUpcomingPublicEventDoesNotInventAffectedAccounts(t *testing.T) {
	api := fakeAPI{
		events: func(*health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
			event := testEvent("public", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 1)
			event.EventScopeCode = ht.EventScopeCodePublic
			return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{event}}, nil
		},
		accounts: func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
			return &health.DescribeAffectedAccountsForOrganizationOutput{EventScopeCode: ht.EventScopeCodePublic}, nil
		},
		details: func(in *health.DescribeEventDetailsForOrganizationInput) (*health.DescribeEventDetailsForOrganizationOutput, error) {
			if in.OrganizationEventDetailFilters[0].AwsAccountId != nil {
				t.Fatal("public event used an account ID")
			}
			return detailResponse(in, "Public guidance"), nil
		},
		resources: func(*health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
			t.Fatal("public event invented resources")
			return nil, nil
		},
	}
	result, err := FetchUpcoming(context.Background(), testQuery(api))
	if err != nil || len(result.Events) != 1 || len(result.Events[0].Accounts) != 0 || result.Events[0].Description != "Public guidance" || !result.Complete {
		t.Fatal(result, err)
	}
}

func TestUpcomingLookupFailuresRetainEvidenceAndMarkIncomplete(t *testing.T) {
	for _, failure := range []string{"accounts", "details", "resources", "description-only"} {
		t.Run(failure, func(t *testing.T) {
			api := fakeAPI{}
			switch failure {
			case "accounts":
				api.accounts = func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
					return nil, errors.New("accounts denied")
				}
			case "resources":
				api.resources = func(*health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
					return &health.DescribeAffectedEntitiesForOrganizationOutput{FailedSet: []ht.OrganizationAffectedEntitiesErrorItem{{ErrorName: aws.String("AccessDenied"), ErrorMessage: aws.String("entities denied")}}}, nil
				}
			case "description-only":
				api.events = func(*health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
					return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{testEvent("event", "OTHER", "RDS", 1)}}, nil
				}
				fallthrough
			case "details":
				api.details = func(*health.DescribeEventDetailsForOrganizationInput) (*health.DescribeEventDetailsForOrganizationOutput, error) {
					return &health.DescribeEventDetailsForOrganizationOutput{FailedSet: []ht.OrganizationEventDetailsErrorItem{{ErrorName: aws.String("AccessDenied")}}}, nil
				}
			}
			result, err := FetchUpcoming(context.Background(), testQuery(api))
			if err != nil || result.Complete || len(result.Warnings) != 1 || result.Warnings[0].EventARN != "event" {
				t.Fatal(result, err)
			}
			if failure == "description-only" {
				if len(result.Events) != 0 {
					t.Fatal(result.Events)
				}
				return
			}
			if len(result.Events) != 1 {
				t.Fatal(result.Events)
			}
			event := result.Events[0]
			if failure == "accounts" {
				if event.AccountsComplete {
					t.Fatal(event)
				}
			} else if !event.AccountsComplete || len(event.Accounts) != 1 || event.Accounts[0].ResourcesComplete != (failure == "details") {
				t.Fatal(event)
			}
		})
	}
}

func TestUpcomingScopesAllExcludedAndCancellation(t *testing.T) {
	api := fakeAPI{events: func(*health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
		return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{testEvent("search", "AWS_ES_PLANNED_LIFECYCLE_EVENT", "ES", 1), testEvent("rds", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 1)}}, nil
	}}
	q := testQuery(api)
	q.Services = []string{"rDs"}
	result, err := FetchUpcoming(context.Background(), q)
	if err != nil || len(result.Events) != 1 || result.Events[0].TypeCode != "AWS_RDS_PLANNED_LIFECYCLE_EVENT" {
		t.Fatal(result, err)
	}
	q.Services, q.Regions = []string{" opensearch ", "ES"}, []string{"us-east-1"}
	result, err = FetchUpcoming(context.Background(), q)
	if err != nil || len(result.Events) != 1 || result.Events[0].ARN != "search" || !reflect.DeepEqual(result.Services, []string{"ES"}) {
		t.Fatal(result, err)
	}
	q.SkipRegions = q.Regions
	q.newClient = func(aws.Config) *Client { t.Fatal("excluded scope queried Health"); return nil }
	result, err = FetchUpcoming(context.Background(), q)
	if err != nil || !result.Complete || len(result.Events) != 0 {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchUpcoming(ctx, q); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := FetchUpcoming(context.Background(), Query{}); err == nil {
		t.Fatal("missing payer accepted")
	}
}

func TestUpcomingConcurrentFailuresKeepOtherAccountsAndEvents(t *testing.T) {
	api := fakeAPI{
		events: func(*health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
			return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{
				testEvent("failed-accounts", "PLANNED_LIFECYCLE_EVENT", "RDS", 3),
				testEvent("mixed-resources", "PLANNED_LIFECYCLE_EVENT", "EKS", 2),
				testEvent("healthy", "PLANNED_LIFECYCLE_EVENT", "LAMBDA", 1),
			}}, nil
		},
		accounts: func(in *health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
			if aws.ToString(in.EventArn) == "failed-accounts" {
				return nil, errors.New("account listing failed")
			}
			ids := []string{testAccount}
			if aws.ToString(in.EventArn) == "mixed-resources" {
				ids = append(ids, "222222222222")
			}
			return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: ids}, nil
		},
		resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
			if aws.ToString(in.OrganizationEntityAccountFilters[0].AwsAccountId) == "222222222222" {
				return nil, errors.New("resource listing failed")
			}
			return fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
		},
	}
	q := testQuery(api)
	q.Workers = 3
	result, err := FetchUpcoming(context.Background(), q)
	if err != nil || result.Complete || len(result.Events) != 3 || len(result.Warnings) != 2 {
		t.Fatal(result, err)
	}
	if result.Events[0].ARN != "healthy" || !result.Events[0].Accounts[0].ResourcesComplete || len(result.Events[0].Accounts[0].Resources) != 1 {
		t.Fatal(result.Events[0])
	}
	mixed := result.Events[1]
	if !mixed.Accounts[0].ResourcesComplete || len(mixed.Accounts[0].Resources) != 1 || mixed.Accounts[1].ResourcesComplete || len(mixed.Accounts[1].Resources) != 0 || result.Events[2].AccountsComplete {
		t.Fatal(result.Events)
	}
}

func TestUpcomingCancellationDuringCollectionReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := fakeAPI{accounts: func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
		cancel()
		return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{testAccount}}, nil
	}}
	if result, err := FetchUpcoming(ctx, testQuery(api)); !errors.Is(err, context.Canceled) || len(result.Events) != 0 {
		t.Fatal(result, err)
	}
}
