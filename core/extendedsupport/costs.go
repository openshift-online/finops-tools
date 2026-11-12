package extendedsupport

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/apilog"
	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/openshift-online/finops-tools/core/cost"
)

// Service names here are exact Cost Explorer SERVICE dimension values. Keep
// aliases for historical billing rather than inferring service from usage text.
var costServiceNames = map[Service][]string{
	ServiceRDS:         {"Amazon Relational Database Service"},
	ServiceEKS:         {"Amazon Elastic Kubernetes Service", "Amazon Elastic Container Service for Kubernetes"},
	ServiceElastiCache: {"Amazon ElastiCache"},
	ServiceOpenSearch:  {"Amazon OpenSearch Service", "Amazon Elasticsearch Service"},
}

// FetchCosts queries historical billing without depending on today's resource
// inventory or account states. Any failed page fails the whole report.
func FetchCosts(ctx context.Context, q CostQuery) (CostResult, error) {
	var err error
	if err = validateCostQuery(&q); err != nil {
		return CostResult{}, err
	}
	result := NewCostResult(q)
	// An explicit selection emptied by exclusions must never become an
	// unrestricted query. No billing lookup is needed for this empty scope.
	if len(q.Regions) > 0 && len(awsregions.Exclude(q.Regions, q.SkipRegions)) == 0 {
		return result, nil
	}
	rows := make(map[string]*CostRow)
	coverage := newCostCoverageTracker(q)
	var serviceTotals map[Service]*CostRow
	if !q.TotalsOnly && !q.allServices {
		serviceTotals = make(map[Service]*CostRow)
	}
	usageTypes := make(map[string]bool)
	services := q.Services
	if q.allServices {
		services = []Service{""}
	}
	groups := cost.GroupByCredentialsAccount(q.Targets)
	for _, id := range cost.SortedCredentialAccountIDs(groups) {
		if err := ctx.Err(); err != nil {
			return CostResult{}, err
		}
		targets := groups[id]
		client := costBillingClient(q, targets[0])
		interval := &cetypes.DateInterval{Start: aws.String(result.StartDate), End: aws.String(cost.FormatDate(q.Range.End))}
		matched, err := listExtendedSupportUsageTypes(ctx, client, interval)
		if err != nil {
			return CostResult{}, fmt.Errorf("billing source %s: %w", id, err)
		}
		for _, accounts := range costAccountBatches(targets) {
			for _, service := range services {
				for offset := 0; offset < len(matched); offset += billingUsageTypeBatchSize {
					batch := matched[offset:min(offset+billingUsageTypeBatchSize, len(matched))]
					if err := fetchServiceCosts(ctx, client, q, service, interval, accounts, batch, &result, rows, serviceTotals, coverage); err != nil {
						if q.allServices {
							return CostResult{}, fmt.Errorf("billing source %s, all services: %w", id, err)
						}
						return CostResult{}, fmt.Errorf("billing source %s, service %s: %w", id, service, err)
					}
				}
			}
		}
		for _, usage := range matched {
			usageTypes[usage] = true
		}
	}
	for usage := range usageTypes {
		result.UsageTypes = append(result.UsageTypes, usage)
	}
	sort.Strings(result.UsageTypes)
	for _, row := range rows {
		result.Rows = append(result.Rows, *row)
		addCostAmounts(&result.CostAmounts, row.CostAmounts)
		result.Estimated = result.Estimated || row.Estimated
	}
	sort.Slice(result.Rows, func(i, j int) bool {
		a, b := result.Rows[i], result.Rows[j]
		if a.StartDate != b.StartDate {
			return a.StartDate < b.StartDate
		}
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		return a.AccountID < b.AccountID
	})
	for _, total := range serviceTotals {
		result.ServiceTotals = append(result.ServiceTotals, *total)
	}
	sort.Slice(result.ServiceTotals, func(i, j int) bool {
		return result.ServiceTotals[i].Service < result.ServiceTotals[j].Service
	})
	result.Coverage = coverage.result()
	if q.allServices {
		result.Notices = append(result.Notices, "Avoidable amounts are unavailable for charges across unclassified services.")
	} else if result.AvoidableAmount == nil {
		result.Notices = append(result.Notices, "EKS Extended Support billing can include the base control-plane fee. Its avoidable portion is unavailable without a verified baseline; known_avoidable_amount excludes it. No fixed savings multiplier is applied.")
	}
	return result, nil
}

func validateCostQuery(q *CostQuery) error {
	q.Regions = awsregions.Normalize(q.Regions)
	q.SkipRegions = awsregions.Normalize(q.SkipRegions)
	if q.Range.Start.IsZero() || q.Range.End.IsZero() || !q.Range.End.After(q.Range.Start) ||
		q.Range.Start != dateOnlyUTC(q.Range.Start) || q.Range.End != dateOnlyUTC(q.Range.End) {
		return fmt.Errorf("cost query requires a non-empty range of UTC calendar dates (start inclusive, end exclusive)")
	}
	if len(q.Targets) == 0 {
		return fmt.Errorf("at least one billing target is required")
	}
	for _, target := range q.Targets {
		if !billingAccountIDPattern.MatchString(target.AccountID) || !billingAccountIDPattern.MatchString(target.CredentialsAccountID()) {
			return fmt.Errorf("billing target must have 12-digit account and credential source IDs")
		}
	}
	if q.allServices {
		q.Services = nil
	} else if len(q.Services) == 0 {
		q.Services, _ = ParseServices("")
	}
	seen := make(map[Service]bool)
	var services []Service
	for _, service := range q.Services {
		if _, ok := costServiceNames[service]; !ok {
			return fmt.Errorf("unknown service %q", service)
		}
		if !seen[service] {
			services = append(services, service)
			seen[service] = true
		}
	}
	q.Services = services
	var err error
	if q.GroupBy, err = ParseCostGroupBy(string(q.GroupBy)); err != nil {
		return err
	}
	if q.allServices && q.GroupBy != CostGroupByAccount {
		return fmt.Errorf("cost queries across all services must group by account")
	}
	if q.Granularity, err = ParseCostGranularity(q.Granularity); err != nil {
		return err
	}
	q.Metric, err = ParseCostMetric(q.Metric)
	return err
}

func costBillingClient(q CostQuery, target cost.AccountTarget) BillingAPI {
	if q.newClient != nil {
		return q.newClient(target)
	}
	cfg := target.AWSConfig.Copy()
	cfg.Region = cost.CostExplorerRegion
	return costexplorer.NewFromConfig(cfg)
}

// An unscoped payer covers the whole bill; otherwise union explicit account IDs.
// A scoped payer must not swallow explicitly selected member accounts.
func costAccountFilter(targets []cost.AccountTarget) *cetypes.Expression {
	var ids []string
	for _, target := range targets {
		if !target.ScopeToAccount() {
			return nil
		}
		ids = append(ids, target.AccountID)
	}
	ids = cost.UniqueAccountIDs(ids)
	sort.Strings(ids)
	return &cetypes.Expression{Dimensions: &cetypes.DimensionValues{Key: cetypes.DimensionLinkedAccount, Values: ids}}
}

func costAccountBatches(targets []cost.AccountTarget) []*cetypes.Expression {
	filter := costAccountFilter(targets)
	if filter == nil {
		return []*cetypes.Expression{nil}
	}
	const batchSize = 100
	ids := filter.Dimensions.Values
	var batches []*cetypes.Expression
	for offset := 0; offset < len(ids); offset += batchSize {
		batches = append(batches, &cetypes.Expression{Dimensions: &cetypes.DimensionValues{
			Key: cetypes.DimensionLinkedAccount, Values: ids[offset:min(offset+batchSize, len(ids))],
		}})
	}
	return batches
}

func fetchServiceCosts(ctx context.Context, client BillingAPI, q CostQuery, service Service, interval *cetypes.DateInterval, accounts *cetypes.Expression, usageTypes []string, result *CostResult, rows map[string]*CostRow, serviceTotals map[Service]*CostRow, coverage *costCoverageTracker) error {
	filters := []cetypes.Expression{
		{Dimensions: &cetypes.DimensionValues{Key: cetypes.DimensionUsageType, Values: usageTypes}},
	}
	if !q.allServices {
		filters = append(filters, cetypes.Expression{Dimensions: &cetypes.DimensionValues{Key: cetypes.DimensionService, Values: costServiceNames[service]}})
	}
	if accounts != nil {
		filters = append(filters, *accounts)
	}
	if regions := supportCostRegionFilter(q); regions != nil {
		filters = append(filters, *regions)
	}
	var token *string
	seen := make(map[string]bool)
	returnedPeriods := make([]bool, coverage.coverage.TotalPeriods)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		input := &costexplorer.GetCostAndUsageInput{
			TimePeriod: interval, Granularity: cetypes.Granularity(strings.ToUpper(q.Granularity)),
			Metrics: []string{q.Metric}, Filter: &cetypes.Expression{And: filters}, NextPageToken: token,
			GroupBy: []cetypes.GroupDefinition{{Type: cetypes.GroupDefinitionTypeDimension, Key: aws.String("LINKED_ACCOUNT")}},
		}
		apilog.LogGetCostAndUsage(ctx, input)
		out, err := client.GetCostAndUsage(ctx, input)
		if err != nil {
			return fmt.Errorf("CostExplorer.GetCostAndUsage: %w", err)
		}
		if out == nil {
			return fmt.Errorf("CostExplorer.GetCostAndUsage returned no response")
		}
		for _, bucket := range out.ResultsByTime {
			result.Estimated = result.Estimated || bucket.Estimated
			start, end, err := costBucketDates(bucket.TimePeriod, q.Range)
			if err != nil {
				return err
			}
			coverage.observe(returnedPeriods, start, end, len(bucket.Groups) > 0)
			if q.TotalsOnly {
				start, end = result.StartDate, result.EndDate
			}
			for _, group := range bucket.Groups {
				if len(group.Keys) != 1 || !billingAccountIDPattern.MatchString(group.Keys[0]) {
					return fmt.Errorf("invalid LINKED_ACCOUNT group %q", group.Keys)
				}
				if accounts != nil && !containsCostAccount(accounts.Dimensions.Values, group.Keys[0]) {
					return fmt.Errorf("billing response contains an account outside the requested scope: %s", group.Keys[0])
				}
				amount, currency, err := parseSupportCost(group.Metrics, q.Metric)
				if err != nil {
					return err
				}
				if result.Currency != "" && result.Currency != currency {
					return fmt.Errorf("cannot combine currencies %s and %s", result.Currency, currency)
				}
				result.Currency = currency
				row := CostRow{StartDate: start, EndDate: end, Estimated: bucket.Estimated, CostAmounts: emptyCostAmounts()}
				if q.GroupBy != CostGroupByAccount {
					row.Service = service
				}
				if q.GroupBy != CostGroupByService {
					row.AccountID = group.Keys[0]
				}
				key := start + "/" + end + "/" + string(row.Service) + "/" + row.AccountID
				if rows[key] == nil {
					rows[key] = &row
				}
				values := CostAmounts{Amount: amount, AvoidableAmount: &amount, KnownAvoidableAmount: amount}
				if q.allServices || service == ServiceEKS {
					values.AvoidableAmount = nil
					values.KnownAvoidableAmount = 0
				}
				addCostAmounts(&rows[key].CostAmounts, values)
				rows[key].Estimated = rows[key].Estimated || bucket.Estimated
				if serviceTotals != nil {
					if serviceTotals[service] == nil {
						serviceTotals[service] = &CostRow{
							StartDate: result.StartDate, EndDate: result.EndDate, Service: service,
							CostAmounts: emptyCostAmounts(),
						}
					}
					addCostAmounts(&serviceTotals[service].CostAmounts, values)
					serviceTotals[service].Estimated = serviceTotals[service].Estimated || bucket.Estimated
				}
			}
		}
		next := aws.ToString(out.NextPageToken)
		if next == "" {
			coverage.finishQuery(returnedPeriods)
			return nil
		}
		if seen[next] {
			return fmt.Errorf("CostExplorer.GetCostAndUsage returned a repeated pagination token")
		}
		seen[next], token = true, out.NextPageToken
	}
}

func supportCostRegionFilter(q CostQuery) *cetypes.Expression {
	if len(q.Regions) > 0 {
		return &cetypes.Expression{Dimensions: &cetypes.DimensionValues{
			Key: cetypes.DimensionRegion, Values: awsregions.Exclude(q.Regions, q.SkipRegions),
		}}
	}
	if len(q.SkipRegions) > 0 {
		return &cetypes.Expression{Not: &cetypes.Expression{Dimensions: &cetypes.DimensionValues{
			Key: cetypes.DimensionRegion, Values: q.SkipRegions,
		}}}
	}
	return nil
}

func parseSupportCost(metrics map[string]cetypes.MetricValue, metric string) (float64, string, error) {
	value, ok := metrics[metric]
	if !ok || value.Amount == nil || strings.TrimSpace(aws.ToString(value.Unit)) == "" {
		return 0, "", fmt.Errorf("billing response is missing %s amount or currency", metric)
	}
	amount, err := strconv.ParseFloat(aws.ToString(value.Amount), 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, "", fmt.Errorf("invalid %s amount %q", metric, aws.ToString(value.Amount))
	}
	return amount, aws.ToString(value.Unit), nil
}

func addCostAmounts(dst *CostAmounts, src CostAmounts) {
	dst.Amount += src.Amount
	dst.KnownAvoidableAmount += src.KnownAvoidableAmount
	if dst.AvoidableAmount == nil || src.AvoidableAmount == nil {
		dst.AvoidableAmount = nil
		return
	}
	value := *dst.AvoidableAmount + *src.AvoidableAmount
	dst.AvoidableAmount = &value
}

func containsCostAccount(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
