package extendedsupport

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/openshift-online/finops-tools/core/cost"
)

const listCostDays = 30
const listCostMetric = "NetUnblendedCost"

// SupportCharges holds historical account/service charges. Nil Amount denotes
// unavailable billing data; a successful query with no charges returns zero.
type SupportCharges struct {
	Amount   *float64 `json:"amount"`
	Currency string   `json:"currency,omitempty"`
	Message  string   `json:"message,omitempty"`
}

type ListCostSummary struct {
	Start           string    `json:"start"`
	EndExclusive    string    `json:"end_exclusive"`
	Metric          string    `json:"metric"`
	AccountIDs      []string  `json:"account_ids"`
	Services        []Service `json:"services"`
	AllServices     bool      `json:"all_services,omitempty"`
	Regions         []string  `json:"regions,omitempty"`
	ExcludedRegions []string  `json:"excluded_regions,omitempty"`
	Notice          string    `json:"notice"`
	Warnings        []string  `json:"warnings,omitempty"`
}

type ListCostQuery struct {
	Targets     []cost.AccountTarget
	Services    []Service
	Regions     []string
	SkipRegions []string
	Now         time.Time
	newClient   func(cost.AccountTarget) BillingAPI
}

// EnrichListCosts attaches account charges and service totals to the existing
// count summaries. It reuses FetchCosts, with explicit account scoping even for
// a payer account. No resource identifiers or resource-level APIs are used.
// Billing failures retain inventory and never become zero or partial totals.
func EnrichListCosts(ctx context.Context, q ListCostQuery, result Result) (Result, error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if q.Now.IsZero() {
		q.Now = time.Now()
	}
	period, err := cost.ResolvePeriod(cost.PeriodSpec{Days: listCostDays}, q.Now)
	if err != nil {
		return result, err
	}
	if len(q.Services) == 0 {
		q.Services, _ = ParseServices("")
	}
	services := make([]Service, 0, len(q.Services))
	for _, service := range q.Services {
		if _, ok := costServiceNames[service]; !ok {
			return result, fmt.Errorf("unknown service %q", service)
		}
		if !slices.Contains(services, service) {
			services = append(services, service)
		}
	}
	q.Services = services
	q.Regions, q.SkipRegions = awsregions.Normalize(q.Regions), awsregions.Normalize(q.SkipRegions)
	metadata := &ListCostSummary{
		Start: cost.FormatDate(period.Start), EndExclusive: cost.FormatDate(period.End), Metric: listCostMetric,
		Services: q.Services, Regions: q.Regions, ExcludedRegions: q.SkipRegions, AccountIDs: []string{},
		Notice: "Extended Support charges cover the last 30 complete UTC days, including the latest two days, which may be incomplete. Costs cover selected services and billing regions for accounts with current findings. Counts reflect current resources; historical account charges may include upgraded or deleted resources.",
	}
	result.Summary.Costs = metadata
	result.Summary.ByAccount = slices.Clone(result.Summary.ByAccount)
	result.Summary.ByService = slices.Clone(result.Summary.ByService)
	accountCounts, serviceCounts := make(map[string]int), make(map[Service]int)
	for _, rec := range result.Records {
		accountCounts[rec.AccountID]++
		serviceCounts[rec.Service]++
	}
	accountCosts := make(map[string]*SupportCharges)
	for id := range accountCounts {
		metadata.AccountIDs = append(metadata.AccountIDs, id)
		accountCosts[id] = &SupportCharges{Message: "Billing credentials are unavailable for this account."}
	}
	sort.Strings(metadata.AccountIDs)
	serviceCosts := make(map[Service]*SupportCharges)
	for _, service := range q.Services {
		serviceCosts[service] = &SupportCharges{Amount: new(float64)}
	}
	var targets []cost.AccountTarget
	for _, target := range q.Targets {
		if accountCounts[target.AccountID] > 0 {
			target.ScopeAccountOnly = true
			targets = append(targets, target)
		}
	}
	groups := cost.GroupByCredentialsAccount(targets)
	for _, source := range cost.SortedCredentialAccountIDs(groups) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		billing, err := FetchCosts(ctx, CostQuery{
			Targets: groups[source], Range: period, Services: q.Services,
			Regions: q.Regions, SkipRegions: q.SkipRegions, GroupBy: CostGroupByServiceAccount,
			Granularity: "daily", TotalsOnly: true, Metric: listCostMetric, newClient: q.newClient,
		})
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			metadata.Warnings = append(metadata.Warnings, err.Error())
			for _, target := range groups[source] {
				accountCosts[target.AccountID] = &SupportCharges{Message: err.Error()}
			}
			continue
		}
		for _, target := range groups[source] {
			accountCosts[target.AccountID] = &SupportCharges{Amount: new(float64), Currency: billing.Currency}
		}
		for _, row := range billing.Rows {
			*accountCosts[row.AccountID].Amount += row.Amount
		}
		for _, service := range q.Services {
			charges := SupportCharges{Amount: new(float64), Currency: billing.Currency}
			for _, row := range billing.Rows {
				if row.Service == service {
					*charges.Amount += row.Amount
				}
			}
			addSupportCharges(serviceCosts[service], charges)
		}
	}
	// A service total is complete only when every account in the scope succeeded.
	for _, charges := range accountCosts {
		if charges.Amount == nil {
			for _, total := range serviceCosts {
				total.Amount = nil
				total.Message = "Service total is unavailable because billing costs could not be retrieved for every account."
			}
			break
		}
	}
	for _, id := range metadata.AccountIDs {
		i := slices.IndexFunc(result.Summary.ByAccount, func(row AccountSummary) bool { return row.AccountID == id })
		if i < 0 {
			result.Summary.ByAccount = append(result.Summary.ByAccount, AccountSummary{AccountID: id, Count: accountCounts[id]})
			i = len(result.Summary.ByAccount) - 1
		}
		result.Summary.ByAccount[i].SupportCost = accountCosts[id]
	}
	for _, service := range q.Services {
		i := slices.IndexFunc(result.Summary.ByService, func(row ServiceSummary) bool { return row.Service == service })
		charges := serviceCosts[service]
		if i < 0 {
			// Retain historically billed services even if no current resource of
			// that service remains in the inventory for these accounts.
			if charges.Amount == nil || *charges.Amount == 0 {
				continue
			}
			result.Summary.ByService = append(result.Summary.ByService, ServiceSummary{Service: service, Count: serviceCounts[service]})
			i = len(result.Summary.ByService) - 1
		}
		result.Summary.ByService[i].SupportCost = charges
		if charges.Amount == nil && charges.Message != "" && len(metadata.Warnings) == 0 {
			metadata.Warnings = append(metadata.Warnings, charges.Message)
		}
	}
	sort.Slice(result.Summary.ByService, func(i, j int) bool { return result.Summary.ByService[i].Service < result.Summary.ByService[j].Service })
	sort.Slice(result.Summary.ByAccount, func(i, j int) bool {
		return result.Summary.ByAccount[i].AccountID < result.Summary.ByAccount[j].AccountID
	})
	return result, nil
}

func addSupportCharges(total *SupportCharges, charges SupportCharges) {
	if total.Amount == nil {
		return
	}
	if total.Currency != "" && charges.Currency != "" && total.Currency != charges.Currency {
		total.Amount = nil
		total.Message = "Service total is unavailable because accounts have different billing currencies."
		return
	}
	if charges.Currency != "" {
		total.Currency = charges.Currency
	}
	*total.Amount += *charges.Amount
}
