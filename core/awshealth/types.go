// Package awshealth collects organizational Health events using caller-provided
// credentials. Its results can be reused for inventory, ownership, and notifications.
package awshealth

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/health"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
)

// OrganizationAPI is the Health subset needed for organizational lifecycle events.
type OrganizationAPI interface {
	DescribeEventsForOrganization(context.Context, *health.DescribeEventsForOrganizationInput, ...func(*health.Options)) (*health.DescribeEventsForOrganizationOutput, error)
	DescribeAffectedAccountsForOrganization(context.Context, *health.DescribeAffectedAccountsForOrganizationInput, ...func(*health.Options)) (*health.DescribeAffectedAccountsForOrganizationOutput, error)
	DescribeEventDetailsForOrganization(context.Context, *health.DescribeEventDetailsForOrganizationInput, ...func(*health.Options)) (*health.DescribeEventDetailsForOrganizationOutput, error)
	DescribeAffectedEntitiesForOrganization(context.Context, *health.DescribeAffectedEntitiesForOrganizationInput, ...func(*health.Options)) (*health.DescribeAffectedEntitiesForOrganizationOutput, error)
}

type Target struct {
	AccountID string
	AWSConfig aws.Config
}

// DefaultRequestInterval is a client pacing policy, not an AWS quota.
const DefaultRequestInterval = 100 * time.Millisecond

type ClientOptions struct {
	// RequestInterval is the minimum spacing between attempts across every API
	// operation on this client. Nonpositive values use DefaultRequestInterval.
	RequestInterval time.Duration
}

type Query struct {
	Targets         []Target
	Now             time.Time
	LookaheadDays   int      // Limit event start times through Now + N UTC days, inclusive; zero is unbounded.
	Services        []string // Health service codes; empty selects every service.
	Regions         []string // Health event regions, including "global" when requested.
	SkipRegions     []string
	HideResolved    bool // Hide RESOLVED resources and accounts/events containing only resolved resources.
	IncludeOwners   bool // Resolve Organizations owner tags using each event's payer credentials.
	Workers         int
	RequestInterval time.Duration // Zero uses DefaultRequestInterval; negative values are invalid.
	newClient       func(aws.Config) *Client
	listTags        func(context.Context, aws.Config, string) ([]coreaccount.Tag, error)
}

// Event retains the Health event ARN separately from affected resource identifiers.
type Event struct {
	PayerAccountID   string            `json:"payer_account_id"`
	ARN              string            `json:"event_arn"`
	TypeCode         string            `json:"event_type_code"`
	Service          string            `json:"service"`
	Region           string            `json:"region"`
	StartTime        time.Time         `json:"start_time"`
	EndTime          *time.Time        `json:"end_time,omitempty"`
	Status           string            `json:"status"`
	Scope            string            `json:"scope"`
	Description      string            `json:"description,omitempty"`
	AccountsComplete bool              `json:"accounts_complete"`
	Accounts         []AffectedAccount `json:"accounts"`
}

type EventDetails struct {
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type AffectedAccount struct {
	AccountID string `json:"account_id"`
	EventDetails
	OwnerEmail        string     `json:"owner_email,omitempty"`
	OwnerError        string     `json:"owner_error,omitempty"`
	ResourcesComplete bool       `json:"resources_complete"`
	Resources         []Resource `json:"resources"`
}

type Resource struct {
	AccountID       string            `json:"account_id"`
	EventARN        string            `json:"event_arn"`
	ResourceID      string            `json:"resource_id"`          // May itself be a resource ARN.
	EntityARN       string            `json:"entity_arn,omitempty"` // Health entity ARN, not the resource ARN.
	URL             string            `json:"url,omitempty"`
	Status          string            `json:"status,omitempty"`
	LastUpdatedTime *time.Time        `json:"last_updated_time,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// Warning identifies an incomplete lookup, even when no matching events remain.
type Warning struct {
	PayerAccountID string `json:"payer_account_id"`
	EventARN       string `json:"event_arn,omitempty"`
	AccountID      string `json:"account_id,omitempty"`
	Operation      string `json:"operation"`
	Message        string `json:"message"`
}

type Result struct {
	AsOf            time.Time `json:"as_of"`
	LookaheadDays   int       `json:"lookahead_days,omitempty"`
	PayerAccountIDs []string  `json:"payer_account_ids"`
	Services        []string  `json:"services,omitempty"`
	Regions         []string  `json:"regions,omitempty"`
	ExcludedRegions []string  `json:"excluded_regions,omitempty"`
	OwnersIncluded  bool      `json:"owners_included,omitempty"`
	Complete        bool      `json:"complete"`
	Events          []Event   `json:"events"`
	Warnings        []Warning `json:"warnings,omitempty"`
}
