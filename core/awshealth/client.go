package awshealth

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/health"
	healthtypes "github.com/aws/aws-sdk-go-v2/service/health/types"
	"github.com/aws/smithy-go/middleware"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// Client exposes independently reusable, fully paginated organizational lookups.
// A failed lookup returns an error rather than an apparently complete partial list.
type Client struct{ api OrganizationAPI }

// NewClient shares request pacing and SDK retry state across all Health operations
// on this client. Explicit caller-provided SDK retry settings are preserved.
func NewClient(cfg aws.Config, optFns ...func(*ClientOptions)) *Client {
	cfg = cfg.Copy()
	cfg.Region = "us-east-1"
	options := ClientOptions{RequestInterval: DefaultRequestInterval}
	for _, fn := range optFns {
		fn(&options)
	}
	if options.RequestInterval <= 0 {
		options.RequestInterval = DefaultRequestInterval
	}
	if cfg.Retryer == nil {
		if cfg.RetryMode == "" {
			cfg.RetryMode = aws.RetryModeAdaptive
		}
		if cfg.RetryMaxAttempts == 0 {
			cfg.RetryMaxAttempts = 6
		}
	}
	pacer := &requestPacer{interval: options.RequestInterval}
	cfg.APIOptions = append(slices.Clone(cfg.APIOptions), func(stack *middleware.Stack) error {
		// Pace inside Retry, after credential retrieval and immediately before
		// signing, so a shared credential refresh cannot release an unpaced burst.
		// One pacer serves every operation/worker.
		return stack.Finalize.Insert(pacer, "Signing", middleware.Before)
	})
	return NewClientWithAPI(health.NewFromConfig(cfg))
}

func NewClientWithAPI(api OrganizationAPI) *Client { return &Client{api: api} }

// ListUpcomingEvents returns scheduled changes at or after now. Matching lifecycle
// text requires event details and is performed separately by FetchUpcoming.
func (c *Client) ListUpcomingEvents(ctx context.Context, now time.Time) ([]Event, error) {
	return c.listUpcomingEvents(ctx, now, nil)
}

// listUpcomingEvents optionally bounds event start times through until, inclusive.
func (c *Client) listUpcomingEvents(ctx context.Context, now time.Time, until *time.Time) ([]Event, error) {
	var events []Event
	seenEvents, seenTokens := make(map[string]bool), make(map[string]bool)
	var token *string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		apilog.Log(ctx, "Health.DescribeEventsForOrganization")
		out, err := c.api.DescribeEventsForOrganization(ctx, &health.DescribeEventsForOrganizationInput{
			Filter: &healthtypes.OrganizationEventFilter{
				EventTypeCategories: []healthtypes.EventTypeCategory{healthtypes.EventTypeCategoryScheduledChange},
				EventStatusCodes:    []healthtypes.EventStatusCode{healthtypes.EventStatusCodeUpcoming, healthtypes.EventStatusCodeOpen},
				StartTime:           &healthtypes.DateTimeRange{From: &now, To: until},
			}, MaxResults: aws.Int32(100), NextToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("Health.DescribeEventsForOrganization: %w", err)
		}
		if out == nil {
			return nil, fmt.Errorf("Health.DescribeEventsForOrganization returned no response")
		}
		for _, item := range out.Events {
			arn := strings.TrimSpace(aws.ToString(item.Arn))
			if arn == "" || item.StartTime == nil || item.StartTime.IsZero() {
				return nil, fmt.Errorf("Health.DescribeEventsForOrganization returned an event without an ARN or start time")
			}
			if item.StartTime.Before(now) || item.StatusCode == healthtypes.EventStatusCodeClosed || item.EventTypeCategory != healthtypes.EventTypeCategoryScheduledChange || seenEvents[arn] {
				continue
			}
			if until != nil && item.StartTime.After(*until) {
				continue
			}
			seenEvents[arn] = true
			events = append(events, Event{ARN: arn, TypeCode: aws.ToString(item.EventTypeCode), Service: aws.ToString(item.Service), Region: aws.ToString(item.Region), StartTime: item.StartTime.UTC(), EndTime: utcTime(item.EndTime), Status: string(item.StatusCode), Scope: string(item.EventScopeCode), Accounts: []AffectedAccount{}})
		}
		if err := nextPage(out.NextToken, seenTokens); err != nil {
			return nil, fmt.Errorf("Health.DescribeEventsForOrganization: %w", err)
		}
		token = out.NextToken
		if aws.ToString(token) == "" {
			return events, nil
		}
	}
}

func (c *Client) AffectedAccounts(ctx context.Context, eventARN string) ([]string, error) {
	accounts, seenTokens := make(map[string]bool), make(map[string]bool)
	var token *string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		apilog.Log(ctx, "Health.DescribeAffectedAccountsForOrganization")
		out, err := c.api.DescribeAffectedAccountsForOrganization(ctx, &health.DescribeAffectedAccountsForOrganizationInput{EventArn: aws.String(eventARN), MaxResults: aws.Int32(100), NextToken: token})
		if err != nil {
			return nil, fmt.Errorf("Health.DescribeAffectedAccountsForOrganization: %w", err)
		}
		if out == nil {
			return nil, fmt.Errorf("Health.DescribeAffectedAccountsForOrganization returned no response")
		}
		if out.EventScopeCode == healthtypes.EventScopeCodeNone {
			return nil, fmt.Errorf("health event is invalid or no longer available")
		}
		for _, id := range out.AffectedAccounts {
			if !validAccountID(id) {
				return nil, fmt.Errorf("health returned invalid affected account %q", id)
			}
			accounts[id] = true
		}
		if err := nextPage(out.NextToken, seenTokens); err != nil {
			return nil, fmt.Errorf("Health.DescribeAffectedAccountsForOrganization: %w", err)
		}
		token = out.NextToken
		if aws.ToString(token) == "" {
			ids := make([]string, 0, len(accounts))
			for id := range accounts {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			return ids, nil
		}
	}
}

// EventDetails accepts an empty accountID only for public Health events.
func (c *Client) EventDetails(ctx context.Context, eventARN, accountID string) (EventDetails, error) {
	if err := ctx.Err(); err != nil {
		return EventDetails{}, err
	}
	filter := healthtypes.EventAccountFilter{EventArn: aws.String(eventARN)}
	if accountID != "" {
		filter.AwsAccountId = aws.String(accountID)
	}
	apilog.Log(ctx, "Health.DescribeEventDetailsForOrganization")
	out, err := c.api.DescribeEventDetailsForOrganization(ctx, &health.DescribeEventDetailsForOrganizationInput{OrganizationEventDetailFilters: []healthtypes.EventAccountFilter{filter}})
	if err != nil {
		return EventDetails{}, fmt.Errorf("Health.DescribeEventDetailsForOrganization: %w", err)
	}
	if out == nil {
		return EventDetails{}, fmt.Errorf("Health.DescribeEventDetailsForOrganization returned no response")
	}
	if len(out.FailedSet) > 0 {
		failure := out.FailedSet[0]
		return EventDetails{}, fmt.Errorf("Health.DescribeEventDetailsForOrganization: %s: %s", aws.ToString(failure.ErrorName), aws.ToString(failure.ErrorMessage))
	}
	if len(out.SuccessfulSet) != 1 {
		return EventDetails{}, fmt.Errorf("Health.DescribeEventDetailsForOrganization returned no unique event details")
	}
	item := out.SuccessfulSet[0]
	if item.Event == nil || aws.ToString(item.Event.Arn) != eventARN || aws.ToString(item.AwsAccountId) != accountID {
		return EventDetails{}, fmt.Errorf("health returned event details outside the requested event/account scope")
	}
	if item.EventDescription == nil || strings.TrimSpace(aws.ToString(item.EventDescription.LatestDescription)) == "" {
		return EventDetails{}, fmt.Errorf("health returned no event description")
	}
	return EventDetails{Description: aws.ToString(item.EventDescription.LatestDescription), Metadata: maps.Clone(item.EventMetadata)}, nil
}

func (c *Client) AffectedResources(ctx context.Context, eventARN, accountID string) ([]Resource, error) {
	var resources []Resource
	seenResources, seenTokens := make(map[string]bool), make(map[string]bool)
	var token *string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		apilog.Log(ctx, "Health.DescribeAffectedEntitiesForOrganization")
		out, err := c.api.DescribeAffectedEntitiesForOrganization(ctx, &health.DescribeAffectedEntitiesForOrganizationInput{
			OrganizationEntityAccountFilters: []healthtypes.EntityAccountFilter{{EventArn: aws.String(eventARN), AwsAccountId: aws.String(accountID)}},
			MaxResults:                       aws.Int32(100), NextToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("Health.DescribeAffectedEntitiesForOrganization: %w", err)
		}
		if out == nil {
			return nil, fmt.Errorf("Health.DescribeAffectedEntitiesForOrganization returned no response")
		}
		if len(out.FailedSet) > 0 {
			failure := out.FailedSet[0]
			return nil, fmt.Errorf("Health.DescribeAffectedEntitiesForOrganization: %s: %s", aws.ToString(failure.ErrorName), aws.ToString(failure.ErrorMessage))
		}
		for _, item := range out.Entities {
			if aws.ToString(item.AwsAccountId) != accountID || aws.ToString(item.EventArn) != eventARN {
				return nil, fmt.Errorf("health returned an entity outside the requested event/account scope")
			}
			id, arn := aws.ToString(item.EntityValue), aws.ToString(item.EntityArn)
			if strings.TrimSpace(id) == "" && strings.TrimSpace(arn) == "" {
				return nil, fmt.Errorf("health returned an entity without an identifier")
			}
			key := arn
			if key == "" {
				key = id
			}
			if seenResources[key] {
				continue
			}
			seenResources[key] = true
			resources = append(resources, Resource{AccountID: accountID, EventARN: eventARN, ResourceID: id, EntityARN: arn, URL: aws.ToString(item.EntityUrl), Status: string(item.StatusCode), LastUpdatedTime: utcTime(item.LastUpdatedTime), Metadata: maps.Clone(item.EntityMetadata)})
		}
		if err := nextPage(out.NextToken, seenTokens); err != nil {
			return nil, fmt.Errorf("Health.DescribeAffectedEntitiesForOrganization: %w", err)
		}
		token = out.NextToken
		if aws.ToString(token) == "" {
			sort.Slice(resources, func(i, j int) bool {
				if resources[i].ResourceID != resources[j].ResourceID {
					return resources[i].ResourceID < resources[j].ResourceID
				}
				return resources[i].EntityARN < resources[j].EntityARN
			})
			return resources, nil
		}
	}
}

func nextPage(token *string, seen map[string]bool) error {
	value := aws.ToString(token)
	if value == "" {
		return nil
	}
	if seen[value] {
		return fmt.Errorf("repeated pagination token")
	}
	seen[value] = true
	return nil
}

func validAccountID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func utcTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	value := t.UTC()
	return &value
}
