package awshealth

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/health"
	ht "github.com/aws/aws-sdk-go-v2/service/health/types"
)

func TestUpcomingHideResolved(t *testing.T) {
	for _, hideResolved := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "hide resolved"}[hideResolved], func(t *testing.T) {
			api := fakeAPI{
				events: func(*health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
					return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{
						testEvent("mixed", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 1),
						testEvent("resolved", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 2),
					}}, nil
				},
				accounts: func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
					return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{testAccount, "222222222222"}}, nil
				},
				resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
					filter := in.OrganizationEntityAccountFilters[0]
					out, _ := fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
					if aws.ToString(filter.EventArn) != "mixed" || aws.ToString(filter.AwsAccountId) != "222222222222" {
						return out, nil
					}
					if in.NextToken == nil {
						out.NextToken = aws.String("more")
						return out, nil
					}
					out.Entities = nil
					for _, status := range []ht.EntityStatusCode{ht.EntityStatusCodePending, ht.EntityStatusCodeImpaired, "", "FUTURE_STATUS"} {
						out.Entities = append(out.Entities, ht.AffectedEntity{
							AwsAccountId: filter.AwsAccountId, EventArn: filter.EventArn,
							EntityValue: aws.String("db-" + string(status)), StatusCode: status,
						})
					}
					return out, nil
				},
			}
			q := testQuery(api)
			q.HideResolved = hideResolved
			result, err := FetchUpcoming(context.Background(), q)
			if err != nil || !result.Complete || len(result.Warnings) != 0 {
				t.Fatal(result, err)
			}
			wantEvents, wantAccounts := 2, 2
			if hideResolved {
				wantEvents, wantAccounts = 1, 1
			}
			if len(result.Events) != wantEvents || result.Events[0].ARN != "mixed" || len(result.Events[0].Accounts) != wantAccounts {
				t.Fatalf("unexpected events or accounts: %+v", result.Events)
			}
			account := result.Events[0].Accounts[wantAccounts-1]
			if account.AccountID != "222222222222" || !account.ResourcesComplete || !result.Events[0].AccountsComplete {
				t.Fatal(account)
			}
			var statuses []string
			for _, resource := range account.Resources {
				statuses = append(statuses, resource.Status)
			}
			wantStatuses := []string{"", "FUTURE_STATUS", "IMPAIRED", "PENDING"}
			if !hideResolved {
				wantStatuses = append(wantStatuses, "RESOLVED")
			}
			if !reflect.DeepEqual(statuses, wantStatuses) {
				t.Fatalf("statuses = %v, want %v", statuses, wantStatuses)
			}
		})
	}
}

func TestUpcomingHideResolvedOmitsFullyResolvedEvents(t *testing.T) {
	q := testQuery(fakeAPI{})
	q.HideResolved = true
	result, err := FetchUpcoming(context.Background(), q)
	if err != nil || !result.Complete || result.Events == nil || len(result.Events) != 0 {
		t.Fatal(result, err)
	}
}

func TestUpcomingHideResolvedPreservesEmptyAndFailedLookups(t *testing.T) {
	for _, lookup := range []string{"empty", "failed", "failed page"} {
		t.Run(lookup, func(t *testing.T) {
			api := fakeAPI{
				accounts: func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
					return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{testAccount, "222222222222"}}, nil
				},
				resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
					if aws.ToString(in.OrganizationEntityAccountFilters[0].AwsAccountId) == testAccount {
						return fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
					}
					if lookup == "empty" {
						return &health.DescribeAffectedEntitiesForOrganizationOutput{}, nil
					}
					if lookup == "failed page" && in.NextToken == nil {
						out, _ := fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
						out.NextToken = aws.String("more")
						return out, nil
					}
					return nil, errors.New("resources denied")
				},
			}
			q := testQuery(api)
			q.HideResolved = true
			result, err := FetchUpcoming(context.Background(), q)
			if err != nil || len(result.Events) != 1 || len(result.Events[0].Accounts) != 1 {
				t.Fatal(result, err)
			}
			account := result.Events[0].Accounts[0]
			complete := lookup == "empty"
			if account.AccountID != "222222222222" || len(account.Resources) != 0 || account.ResourcesComplete != complete || result.Complete != complete {
				t.Fatal(result, account)
			}
			if !complete && (len(result.Warnings) != 1 || result.Warnings[0].AccountID != account.AccountID || result.Warnings[0].Operation != "affected_resources") {
				t.Fatal(result.Warnings)
			}
		})
	}
}

func TestUpcomingHideResolvedPreservesWarningsForHiddenAccounts(t *testing.T) {
	api := fakeAPI{
		accounts: func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
			return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{testAccount, "222222222222"}}, nil
		},
		details: func(in *health.DescribeEventDetailsForOrganizationInput) (*health.DescribeEventDetailsForOrganizationOutput, error) {
			if aws.ToString(in.OrganizationEventDetailFilters[0].AwsAccountId) == testAccount {
				return nil, errors.New("details denied")
			}
			return detailResponse(in, "Extended Support begins soon."), nil
		},
	}
	q := testQuery(api)
	q.HideResolved = true
	result, err := FetchUpcoming(context.Background(), q)
	if err != nil || result.Complete || len(result.Events) != 0 || len(result.Warnings) != 1 || result.Warnings[0].AccountID != testAccount {
		t.Fatal(result, err)
	}
}
