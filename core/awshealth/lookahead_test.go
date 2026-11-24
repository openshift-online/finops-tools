package awshealth

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/health"
	ht "github.com/aws/aws-sdk-go-v2/service/health/types"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
)

func TestUpcomingLookaheadBoundsStartTimesBeforeCollectingFindings(t *testing.T) {
	for _, tc := range []struct {
		name string
		days int
		want []string
	}{
		{name: "unbounded", want: []string{"start", "inside", "last", "cutoff", "after"}},
		{name: "one day", days: 1, want: []string{"start", "inside"}},
		{name: "ninety days", days: 90, want: []string{"start", "inside", "last", "cutoff"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cutoff := testNow.AddDate(0, 0, 90)
			eventAt := func(arn string, start time.Time) ht.OrganizationEvent {
				event := testEvent(arn, "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 0)
				event.StartTime = &start
				// Only the start time determines whether the event is in the window.
				end := cutoff.AddDate(0, 0, 30)
				event.EndTime = &end
				return event
			}
			pages, accounts, resources, owners := 0, 0, 0, 0
			api := fakeAPI{
				events: func(in *health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
					pages++
					window := in.Filter.StartTime
					if !window.From.Equal(testNow) {
						t.Fatal("unexpected window start:", window)
					}
					if tc.days == 0 {
						if window.To != nil {
							t.Fatal("default query added an upper bound:", window)
						}
					} else if window.To == nil || !window.To.Equal(testNow.AddDate(0, 0, tc.days)) {
						t.Fatal("unexpected window end:", window)
					}
					if in.NextToken == nil {
						// AWS results are not assumed to be ordered or prefiltered.
						return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{
							eventAt("after", cutoff.Add(time.Nanosecond)),
							eventAt("past", testNow.Add(-time.Nanosecond)),
						}, NextToken: aws.String("more")}, nil
					}
					return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{
						eventAt("start", testNow),
						eventAt("inside", testNow.AddDate(0, 0, 1)),
						eventAt("last", cutoff.Add(-time.Nanosecond)),
						eventAt("cutoff", cutoff),
					}}, nil
				},
				accounts: func(in *health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
					accounts++
					arn := aws.ToString(in.EventArn)
					allowed := false
					for _, want := range tc.want {
						allowed = allowed || arn == want
					}
					if !allowed {
						t.Fatalf("collected accounts for an out-of-window event: %s", arn)
					}
					return fakeAPI{}.DescribeAffectedAccountsForOrganization(context.Background(), in)
				},
				resources: func(in *health.DescribeAffectedEntitiesForOrganizationInput) (*health.DescribeAffectedEntitiesForOrganizationOutput, error) {
					resources++
					out, err := fakeAPI{}.DescribeAffectedEntitiesForOrganization(context.Background(), in)
					out.Entities[0].StatusCode = ht.EntityStatusCodePending
					return out, err
				},
			}
			q := testQuery(api)
			q.Now = testNow.In(time.FixedZone("UTC-5", -5*60*60))
			q.LookaheadDays, q.IncludeOwners, q.HideResolved = tc.days, true, true
			q.listTags = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
				owners++
				return []coreaccount.Tag{{Key: "owner", Value: "jdoe"}}, nil
			}
			result, err := FetchUpcoming(context.Background(), q)
			if err != nil || !result.Complete || result.LookaheadDays != tc.days || !result.AsOf.Equal(testNow) {
				t.Fatal(result, err)
			}
			var arns []string
			for _, event := range result.Events {
				arns = append(arns, event.ARN)
				if event.Accounts[0].OwnerEmail != "jdoe@redhat.com" || len(event.Accounts[0].Resources) != 1 {
					t.Fatal(event)
				}
			}
			if !reflect.DeepEqual(arns, tc.want) || pages != 2 || accounts != len(tc.want) || resources != len(tc.want) || owners != 1 {
				t.Fatalf("events=%v pages=%d accounts=%d resources=%d owners=%d", arns, pages, accounts, resources, owners)
			}
		})
	}
}

func TestUpcomingLookaheadEmptyWindowAndInvalidInput(t *testing.T) {
	api := fakeAPI{
		events: func(*health.DescribeEventsForOrganizationInput) (*health.DescribeEventsForOrganizationOutput, error) {
			return &health.DescribeEventsForOrganizationOutput{Events: []ht.OrganizationEvent{testEvent("later", "AWS_RDS_PLANNED_LIFECYCLE_EVENT", "RDS", 91)}}, nil
		},
		accounts: func(*health.DescribeAffectedAccountsForOrganizationInput) (*health.DescribeAffectedAccountsForOrganizationOutput, error) {
			t.Fatal("out-of-window accounts queried")
			return nil, nil
		},
	}
	q := testQuery(api)
	q.LookaheadDays, q.IncludeOwners = 90, true
	q.listTags = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		t.Fatal("out-of-window owners queried")
		return nil, nil
	}
	result, err := FetchUpcoming(context.Background(), q)
	if err != nil || !result.Complete || result.Events == nil || len(result.Events) != 0 || len(result.Warnings) != 0 {
		t.Fatal(result, err)
	}
	q.LookaheadDays = -1
	q.newClient = func(aws.Config) *Client {
		t.Fatal("invalid lookahead queried AWS")
		return nil
	}
	if _, err := FetchUpcoming(context.Background(), q); err == nil {
		t.Fatal("negative lookahead accepted")
	}
}
