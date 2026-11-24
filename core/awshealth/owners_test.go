package awshealth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/health"
	ht "github.com/aws/aws-sdk-go-v2/service/health/types"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
)

func pendingOwnerAPI() fakeAPI {
	return fakeAPI{resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
		out, err := fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
		out.Entities[0].StatusCode = ht.EntityStatusCodePending
		return out, err
	}}
}

func TestUpcomingOwnersUseOrganizationsAccountTags(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Amz-Target") != "AWSOrganizationsV20161128.ListTagsForResource" || !strings.Contains(r.Header.Get("Authorization"), "payer-access-key/") {
			t.Errorf("unexpected request: %v", r.Header)
		}
		var input struct{ ResourceId, NextToken string }
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.ResourceId != testAccount {
			t.Errorf("unexpected account tag request: %+v, %v", input, err)
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if input.NextToken == "" {
			_, _ = io.WriteString(w, `{"Tags":[{"Key":"team","Value":"finops"}],"NextToken":"more"}`)
		} else {
			_, _ = io.WriteString(w, `{"Tags":[{"Key":"owner","Value":"Jane Doe <JDOE@REDHAT.COM>"}]}`)
		}
	}))
	defer server.Close()
	q := testQuery(pendingOwnerAPI())
	q.IncludeOwners = true
	q.Targets[0].AWSConfig = aws.Config{
		Region: "us-east-1", BaseEndpoint: aws.String(server.URL),
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "payer-access-key", SecretAccessKey: "test-secret"}, nil
		}),
	}
	result, err := FetchUpcoming(context.Background(), q)
	if err != nil || !result.Complete || !result.OwnersIncluded || len(result.Events) != 1 || calls.Load() != 2 {
		t.Fatal(result, calls.Load(), err)
	}
	account := result.Events[0].Accounts[0]
	if account.OwnerEmail != "jdoe@redhat.com" || account.OwnerError != "" || account.Resources[0].Status != "PENDING" {
		t.Fatal(account)
	}
}

func TestUpcomingOwnersDeduplicateAccountsPerPayer(t *testing.T) {
	api := pendingOwnerAPI()
	api.events = func(*health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
		return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{
			testEvent("first", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 1),
			testEvent("second", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 2),
		}}, nil
	}
	q := testQuery(api)
	q.IncludeOwners = true
	q.Targets = []Target{
		{AccountID: "123456789012", AWSConfig: aws.Config{Region: "payer-a"}},
		{AccountID: "987654321098", AWSConfig: aws.Config{Region: "payer-b"}},
	}
	calls := make(map[string]int)
	q.listTags = func(_ context.Context, cfg aws.Config, id string) ([]coreaccount.Tag, error) {
		if id != testAccount {
			t.Fatalf("owner lookup for unexpected account %s", id)
		}
		calls[cfg.Region]++
		return []coreaccount.Tag{{Key: "OWNER", Value: cfg.Region}}, nil
	}
	result, err := FetchUpcoming(context.Background(), q)
	if err != nil || !result.Complete || len(result.Events) != 4 || calls["payer-a"] != 1 || calls["payer-b"] != 1 || len(calls) != 2 {
		t.Fatal(result, calls, err)
	}
	for _, event := range result.Events {
		want := "payer-a@redhat.com"
		if event.PayerAccountID == "987654321098" {
			want = "payer-b@redhat.com"
		}
		if event.Accounts[0].OwnerEmail != want || event.Description == "" || event.Accounts[0].Resources[0].ResourceID != "db-production" {
			t.Fatal(event)
		}
	}
}

func TestUpcomingOwnersLookupOnlyAccountsRemainingAfterFiltering(t *testing.T) {
	for _, allResolved := range []bool{false, true} {
		api := fakeAPI{
			accounts: func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
				return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{testAccount, "222222222222"}}, nil
			},
			resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
				out, err := fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
				if !allResolved && aws.ToString(in.OrganizationEntityAccountFilters[0].AwsAccountId) == "222222222222" {
					out.Entities[0].StatusCode = ht.EntityStatusCodePending
				}
				return out, err
			},
		}
		q := testQuery(api)
		q.IncludeOwners, q.HideResolved = true, true
		calls := 0
		q.listTags = func(_ context.Context, _ aws.Config, id string) ([]coreaccount.Tag, error) {
			calls++
			if allResolved || id != "222222222222" {
				t.Fatalf("looked up a filtered account: %s", id)
			}
			return []coreaccount.Tag{{Key: "owner", Value: "jdoe"}}, nil
		}
		result, err := FetchUpcoming(context.Background(), q)
		if err != nil || !result.Complete || !result.OwnersIncluded {
			t.Fatal(result, err)
		}
		if allResolved {
			if calls != 0 || len(result.Events) != 0 {
				t.Fatal(result, calls)
			}
		} else if calls != 1 || len(result.Events) != 1 || len(result.Events[0].Accounts) != 1 || result.Events[0].Accounts[0].OwnerEmail != "jdoe@redhat.com" {
			t.Fatal(result, calls)
		}
	}
}

func TestUpcomingOwnerFailuresKeepFindingsAndOtherOwners(t *testing.T) {
	for _, tc := range []struct {
		name, value, message string
		lookupErr            error
	}{
		{name: "missing", message: "owner tag missing"},
		{name: "empty", message: "owner tag empty"},
		{name: "invalid", value: "not a mailbox@", message: "owner email invalid"},
		{name: "access denied", message: "list account tags: denied", lookupErr: errors.New("denied")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := pendingOwnerAPI()
			api.accounts = func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
				return &health.DescribeAffectedAccountsForOrganizationOutput{AffectedAccounts: []string{testAccount, "222222222222"}}, nil
			}
			q := testQuery(api)
			q.IncludeOwners, q.Workers = true, 2
			q.listTags = func(_ context.Context, _ aws.Config, id string) ([]coreaccount.Tag, error) {
				if id == "222222222222" {
					return []coreaccount.Tag{{Key: "owner", Value: "jdoe"}}, nil
				}
				if tc.name == "missing" {
					return nil, nil
				}
				return []coreaccount.Tag{{Key: "owner", Value: tc.value}}, tc.lookupErr
			}
			result, err := FetchUpcoming(context.Background(), q)
			if err != nil || result.Complete || len(result.Events) != 1 || len(result.Events[0].Accounts) != 2 || len(result.Warnings) != 1 {
				t.Fatal(result, err)
			}
			failed, healthy := result.Events[0].Accounts[0], result.Events[0].Accounts[1]
			if failed.OwnerEmail != "" || !strings.Contains(failed.OwnerError, tc.message) || len(failed.Resources) != 1 || healthy.OwnerEmail != "jdoe@redhat.com" {
				t.Fatal(failed, healthy)
			}
			warning := result.Warnings[0]
			if warning.Operation != "account_owner" || warning.AccountID != testAccount || warning.EventARN != "event" || warning.PayerAccountID != q.Targets[0].AccountID {
				t.Fatal(warning)
			}
		})
	}
}

func TestUpcomingOwnerLookupIsOptionalAndCancellationAborts(t *testing.T) {
	q := testQuery(pendingOwnerAPI())
	q.listTags = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		t.Fatal("owner lookup without IncludeOwners")
		return nil, nil
	}
	result, err := FetchUpcoming(context.Background(), q)
	if err != nil || result.OwnersIncluded || result.Events[0].Accounts[0].OwnerEmail != "" {
		t.Fatal(result, err)
	}
	q.IncludeOwners = true
	for _, abort := range []error{context.Canceled, context.DeadlineExceeded} {
		q.listTags = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) { return nil, abort }
		if _, err := FetchUpcoming(context.Background(), q); !errors.Is(err, abort) {
			t.Fatalf("got %v, want %v", err, abort)
		}
	}
}
