package extendedsupport

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/apilog"
	"github.com/openshift-online/finops-tools/core/cost"
)

const billingUsageTypeBatchSize = 100

var billingAccountIDPattern = regexp.MustCompile(`^[0-9]{12}$`)

// BillingAPI is the Cost Explorer subset used to discover candidate accounts.
type BillingAPI interface {
	GetDimensionValues(context.Context, *costexplorer.GetDimensionValuesInput, ...func(*costexplorer.Options)) (*costexplorer.GetDimensionValuesOutput, error)
	GetCostAndUsage(context.Context, *costexplorer.GetCostAndUsageInput, ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}

// BillingCandidates identifies accounts with recorded Extended Support usage in
// Period. Billing data is historical evidence; callers must scan live resources
// to confirm their current support status.
type BillingCandidates struct {
	Period     cost.DateRange
	UsageTypes []string
	AccountIDs []string
}

// BillingSelection describes the historical evidence used to narrow a live scan.
// Accounts outside this selection are not scan failures or skipped accounts.
type BillingSelection struct {
	PayerAccountID       string   `json:"payer_account_id"`
	Start                string   `json:"start"`
	EndExclusive         string   `json:"end_exclusive"`
	UsageTypes           []string `json:"usage_types"`
	BilledAccountCount   int      `json:"billed_account_count"`
	EligibleAccountCount int      `json:"eligible_account_count"`
	SelectedAccountCount int      `json:"selected_account_count"`
	Notice               string   `json:"notice"`
}

// BillingLookbackRange includes today and the preceding days-1 UTC calendar days.
func BillingLookbackRange(now time.Time, days int) (cost.DateRange, error) {
	if days < 1 {
		return cost.DateRange{}, fmt.Errorf("billing lookback days must be at least 1")
	}
	if now.IsZero() {
		now = time.Now()
	}
	year, month, day := now.UTC().Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return cost.DateRange{Start: today.AddDate(0, 0, 1-days), End: today.AddDate(0, 0, 1)}, nil
}

// DiscoverBillingCandidates queries the payer's Cost Explorer data, matching
// usage types containing "extendedsupport" without regard to case. Every matched
// linked-account group is retained, including zero or negative cost amounts.
// Discovery errors return no partial candidate list.
func DiscoverBillingCandidates(ctx context.Context, cfg aws.Config, period cost.DateRange) (BillingCandidates, error) {
	regionalCfg := cfg.Copy()
	regionalCfg.Region = cost.CostExplorerRegion
	return discoverBillingCandidatesWithClient(ctx, costexplorer.NewFromConfig(regionalCfg), period)
}

func discoverBillingCandidatesWithClient(ctx context.Context, client BillingAPI, period cost.DateRange) (BillingCandidates, error) {
	start, end := period.Start.UTC().Format("2006-01-02"), period.End.UTC().Format("2006-01-02")
	if period.Start.IsZero() || period.End.IsZero() || start >= end {
		return BillingCandidates{}, fmt.Errorf("billing discovery requires a non-empty start-inclusive, end-exclusive date range")
	}
	interval := &cetypes.DateInterval{Start: aws.String(start), End: aws.String(end)}
	usageTypes, err := listExtendedSupportUsageTypes(ctx, client, interval)
	if err != nil {
		return BillingCandidates{}, err
	}
	accounts := make(map[string]bool)
	for offset := 0; offset < len(usageTypes); offset += billingUsageTypeBatchSize {
		end := min(offset+billingUsageTypeBatchSize, len(usageTypes))
		if err := listBilledExtendedSupportAccounts(ctx, client, interval, usageTypes[offset:end], accounts); err != nil {
			return BillingCandidates{}, err
		}
	}
	ids := make([]string, 0, len(accounts))
	for id := range accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return BillingCandidates{Period: period, UsageTypes: usageTypes, AccountIDs: ids}, nil
}

func listExtendedSupportUsageTypes(ctx context.Context, client BillingAPI, interval *cetypes.DateInterval) ([]string, error) {
	matched := make(map[string]bool)
	seenTokens := make(map[string]bool)
	var token *string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		input := &costexplorer.GetDimensionValuesInput{
			Dimension: cetypes.DimensionUsageType, Context: cetypes.ContextCostAndUsage,
			TimePeriod: interval, NextPageToken: token,
		}
		apilog.LogGetDimensionValues(ctx, input)
		out, err := client.GetDimensionValues(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("CostExplorer.GetDimensionValues for Extended Support usage types: %w", err)
		}
		if out == nil {
			return nil, fmt.Errorf("CostExplorer.GetDimensionValues returned no response")
		}
		for _, dimension := range out.DimensionValues {
			value := aws.ToString(dimension.Value)
			if strings.Contains(strings.ToLower(value), "extendedsupport") {
				matched[value] = true
			}
		}
		next := aws.ToString(out.NextPageToken)
		if next == "" {
			break
		}
		if seenTokens[next] {
			return nil, fmt.Errorf("CostExplorer.GetDimensionValues returned a repeated pagination token")
		}
		seenTokens[next] = true
		token = out.NextPageToken
	}
	values := make([]string, 0, len(matched))
	for value := range matched {
		values = append(values, value)
	}
	sort.Strings(values)
	return values, nil
}

func listBilledExtendedSupportAccounts(ctx context.Context, client BillingAPI, interval *cetypes.DateInterval, usageTypes []string, accounts map[string]bool) error {
	seenTokens := make(map[string]bool)
	var token *string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		input := &costexplorer.GetCostAndUsageInput{
			TimePeriod: interval, Granularity: cetypes.GranularityMonthly,
			Metrics: []string{"UnblendedCost"}, NextPageToken: token,
			Filter: &cetypes.Expression{Dimensions: &cetypes.DimensionValues{
				Key: cetypes.DimensionUsageType, Values: usageTypes,
				MatchOptions: []cetypes.MatchOption{cetypes.MatchOptionEquals},
			}},
			GroupBy: []cetypes.GroupDefinition{{Type: cetypes.GroupDefinitionTypeDimension, Key: aws.String("LINKED_ACCOUNT")}},
		}
		apilog.LogGetCostAndUsage(ctx, input)
		out, err := client.GetCostAndUsage(ctx, input)
		if err != nil {
			return fmt.Errorf("CostExplorer.GetCostAndUsage for Extended Support accounts: %w", err)
		}
		if out == nil {
			return fmt.Errorf("CostExplorer.GetCostAndUsage returned no response")
		}
		for _, period := range out.ResultsByTime {
			for _, group := range period.Groups {
				if len(group.Keys) != 1 || !billingAccountIDPattern.MatchString(group.Keys[0]) {
					return fmt.Errorf("CostExplorer.GetCostAndUsage returned invalid LINKED_ACCOUNT group %q", group.Keys)
				}
				accounts[group.Keys[0]] = true
			}
		}
		next := aws.ToString(out.NextPageToken)
		if next == "" {
			return nil
		}
		if seenTokens[next] {
			return fmt.Errorf("CostExplorer.GetCostAndUsage returned a repeated pagination token")
		}
		seenTokens[next] = true
		token = out.NextPageToken
	}
}
