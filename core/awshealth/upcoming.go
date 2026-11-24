package awshealth

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/openshift-online/finops-tools/core/parallel"
)

// NormalizeServices accepts Health service codes and familiar discovery aliases.
func NormalizeServices(services []string) []string {
	var out []string
	for _, service := range services {
		service = strings.ToUpper(strings.TrimSpace(service))
		if service == "OPENSEARCH" || service == "ELASTICSEARCH" {
			service = "ES"
		}
		if service != "" && !slices.Contains(out, service) {
			out = append(out, service)
		}
	}
	sort.Strings(out)
	return out
}

// FetchUpcoming collects planned lifecycle events whose descriptions mention
// extended support, along with their affected accounts and resources, without
// member-role assumption or billing queries. Failure to list a payer's events
// fails the request. Individual lookup failures produce structured warnings and
// Complete=false; an event requires a matching description to be included.
// IncludeOwners resolves account owner tags after filtering with payer credentials.
func FetchUpcoming(ctx context.Context, q Query) (Result, error) {
	if q.LookaheadDays < 0 {
		return Result{}, fmt.Errorf("lookahead days must not be negative")
	}
	if q.RequestInterval < 0 {
		return Result{}, fmt.Errorf("health request interval must not be negative")
	}
	if len(q.Targets) == 0 {
		return Result{}, fmt.Errorf("at least one organization Health target is required")
	}
	for _, target := range q.Targets {
		if !validAccountID(target.AccountID) {
			return Result{}, fmt.Errorf("health target requires a 12-digit account ID")
		}
	}
	if q.Now.IsZero() {
		q.Now = time.Now()
	}
	q.Now = q.Now.UTC()
	var until *time.Time
	if q.LookaheadDays > 0 {
		end := q.Now.AddDate(0, 0, q.LookaheadDays)
		until = &end
	}
	q.Services = NormalizeServices(q.Services)
	q.Regions, q.SkipRegions = awsregions.Normalize(q.Regions), awsregions.Normalize(q.SkipRegions)
	result := Result{
		AsOf: q.Now, LookaheadDays: q.LookaheadDays, PayerAccountIDs: []string{}, Services: q.Services,
		Regions: q.Regions, ExcludedRegions: q.SkipRegions, OwnersIncluded: q.IncludeOwners,
		Complete: true, Events: []Event{},
	}
	seen := make(map[string]bool)
	for _, target := range q.Targets {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if seen[target.AccountID] {
			continue
		}
		seen[target.AccountID] = true
		result.PayerAccountIDs = append(result.PayerAccountIDs, target.AccountID)
		if len(q.Regions) > 0 && len(awsregions.Exclude(q.Regions, q.SkipRegions)) == 0 {
			continue
		}
		var client *Client
		if q.newClient != nil {
			client = q.newClient(target.AWSConfig)
		} else {
			client = NewClient(target.AWSConfig, func(options *ClientOptions) {
				options.RequestInterval = q.RequestInterval
			})
		}
		events, err := client.listUpcomingEvents(ctx, q.Now, until)
		if err != nil {
			return Result{}, fmt.Errorf("health event discovery for payer %s: %w", target.AccountID, err)
		}
		var selected []Event
		for _, event := range events {
			if len(q.Services) > 0 && !slices.Contains(q.Services, strings.ToUpper(event.Service)) {
				continue
			}
			if len(q.Regions) > 0 && !slices.Contains(q.Regions, event.Region) {
				continue
			}
			if slices.Contains(q.SkipRegions, event.Region) {
				continue
			}
			event.PayerAccountID = target.AccountID
			selected = append(selected, event)
		}
		collected := make([]*Event, len(selected))
		warnings := make([][]Warning, len(selected))
		err = parallel.ForEach(ctx, q.Workers, len(selected), func(ctx context.Context, i int) error {
			collected[i], warnings[i] = collectEvent(ctx, client, selected[i])
			return ctx.Err()
		})
		if err != nil {
			return Result{}, err
		}
		for i, event := range collected {
			if event != nil && q.HideResolved {
				event = hideResolvedResources(event)
			}
			if event != nil {
				result.Events = append(result.Events, *event)
			}
			result.Warnings = append(result.Warnings, warnings[i]...)
		}
	}
	if q.IncludeOwners {
		if err := resolveAccountOwners(ctx, q, &result); err != nil {
			return Result{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sort.Strings(result.PayerAccountIDs)
	sort.Slice(result.Events, func(i, j int) bool {
		a, b := result.Events[i], result.Events[j]
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		if a.ARN != b.ARN {
			return a.ARN < b.ARN
		}
		return a.PayerAccountID < b.PayerAccountID
	})
	sort.Slice(result.Warnings, func(i, j int) bool {
		a, b := result.Warnings[i], result.Warnings[j]
		return a.PayerAccountID+"/"+a.EventARN+"/"+a.AccountID+"/"+a.Operation < b.PayerAccountID+"/"+b.EventARN+"/"+b.AccountID+"/"+b.Operation
	})
	result.Complete = len(result.Warnings) == 0
	return result, nil
}

func collectEvent(ctx context.Context, client *Client, event Event) (*Event, []Warning) {
	var warnings []Warning
	warn := func(operation, accountID string, err error) {
		warnings = append(warnings, Warning{PayerAccountID: event.PayerAccountID, EventARN: event.ARN, AccountID: accountID, Operation: operation, Message: err.Error()})
	}
	matchedLifecycle := plannedLifecycleText(event.TypeCode)
	matchedDescription := false
	accounts, err := client.AffectedAccounts(ctx, event.ARN)
	if err != nil {
		warn("affected_accounts", "", err)
		return nil, warnings
	}
	event.AccountsComplete = true
	// Public events have no affected account list and require a null account ID
	// when requesting their description. Never invent an affected payer account.
	detailAccounts := accounts
	if len(accounts) == 0 && event.Scope == "PUBLIC" {
		detailAccounts = []string{""}
	}
	for _, id := range detailAccounts {
		account := AffectedAccount{AccountID: id, Resources: []Resource{}}
		details, err := client.EventDetails(ctx, event.ARN, id)
		if err != nil {
			warn("event_details", id, err)
		} else {
			account.EventDetails = details
			matchesDescription := extendedSupportText(details.Description)
			if matchesDescription && !matchedDescription {
				event.Description = details.Description
			}
			matchedDescription = matchedDescription || matchesDescription
			matchedLifecycle = matchedLifecycle || plannedLifecycleText(details.Description)
		}
		if id != "" {
			event.Accounts = append(event.Accounts, account)
		}
	}
	if !matchedLifecycle || !matchedDescription {
		return nil, warnings
	}
	for i := range event.Accounts {
		account := &event.Accounts[i]
		resources, err := client.AffectedResources(ctx, event.ARN, account.AccountID)
		if err != nil {
			warn("affected_resources", account.AccountID, err)
			continue
		}
		account.ResourcesComplete = true
		if resources != nil {
			account.Resources = resources
		}
	}
	return &event, warnings
}

// hideResolvedResources removes only confirmed resolved content. Empty or
// incomplete lookups remain visible because they do not establish resolution.
func hideResolvedResources(event *Event) *Event {
	hadAccounts := len(event.Accounts) > 0
	accounts := event.Accounts[:0]
	for _, account := range event.Accounts {
		hadResources := len(account.Resources) > 0
		account.Resources = slices.DeleteFunc(account.Resources, func(resource Resource) bool {
			return strings.EqualFold(strings.TrimSpace(resource.Status), "RESOLVED")
		})
		if hadResources && len(account.Resources) == 0 && account.ResourcesComplete {
			continue
		}
		accounts = append(accounts, account)
	}
	clear(event.Accounts[len(accounts):])
	event.Accounts = accounts
	if hadAccounts && len(event.Accounts) == 0 && event.AccountsComplete {
		return nil
	}
	return event
}

func plannedLifecycleText(value string) bool {
	value = strings.NewReplacer("_", " ", "-", " ").Replace(strings.ToLower(value))
	return strings.Contains(strings.Join(strings.Fields(value), " "), "planned lifecycle event")
}

func extendedSupportText(value string) bool {
	return strings.Contains(strings.Join(strings.Fields(strings.ToLower(value)), " "), "extended support")
}
