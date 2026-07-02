// Package cost fetches and aggregates cloud cost data from provider APIs using caller-supplied credentials.
package cost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// Provider identifies a cloud cost data source.
type Provider string

const (
	ProviderAWS Provider = "aws"
	ProviderGCP Provider = "gcp"
)

// DefaultDays is the lookback window for account get-cost.
const DefaultDays = 30

// MetricNetAmortized is the AWS Cost Explorer metric name.
const MetricNetAmortized = "NetAmortizedCost"

// SplitBy identifies how cost results are grouped.
type SplitBy string

const (
	SplitByNone    SplitBy = ""
	SplitByService SplitBy = "service"
	SplitByAccount SplitBy = "account"
)

var errProviderNotImplemented = errors.New("cost provider not implemented")

// ParseSplitBy parses a --split-by flag value (case-insensitive). Empty means no split.
func ParseSplitBy(s string) (SplitBy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return SplitByNone, nil
	case string(SplitByService):
		return SplitByService, nil
	case string(SplitByAccount):
		return SplitByAccount, nil
	default:
		return "", fmt.Errorf("unknown split-by %q (supported: service, account)", s)
	}
}

// ParseProvider parses a provider flag value (case-insensitive).
func ParseProvider(s string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(ProviderAWS), "":
		return ProviderAWS, nil
	case string(ProviderGCP):
		return ProviderGCP, nil
	default:
		return "", fmt.Errorf("unknown provider %q (supported: aws, gcp)", s)
	}
}

// ResolveAWSAccountNamesFunc maps specific account IDs to display names (Organizations).
type ResolveAWSAccountNamesFunc func(context.Context, aws.Config, []string) (map[string]string, error)

// AWSFetchOptions configures optional AWS-specific behavior for cost fetch.
type AWSFetchOptions struct {
	// ListAccountNames loads all organization accounts (slow on large orgs).
	// Prefer ResolveAccountNames when available.
	ListAccountNames ListAWSAccountNamesFunc
	// ResolveAccountNames looks up only the given account IDs (fast for small sets).
	ResolveAccountNames ResolveAWSAccountNamesFunc
}

// FetchProgress reports long-running steps while fetching costs.
type FetchProgress interface {
	Step(message string)
}

// CostQuery describes a cost fetch request.
type CostQuery struct {
	Provider Provider
	Accounts []AccountTarget
	Range    DateRange
	SplitBy  SplitBy
	AWSFetch *AWSFetchOptions
	Progress FetchProgress
}

// AccountTarget identifies an AWS account whose costs are fetched.
type AccountTarget struct {
	// AccountID is the 12-digit account ID whose costs are reported.
	AccountID string
	// PayerAccountID is set when AccountID is a linked (member) account.
	PayerAccountID string
	// AWSConfig holds authenticated payer credentials for Cost Explorer (set by the CLI).
	AWSConfig aws.Config
	// DisplayName is the AWS Organizations account name when resolved by the CLI.
	DisplayName string
	// DisplayAlias is the configured finops alias when the target was selected by alias.
	DisplayAlias string
	// ScopeAccountOnly forces a LINKED_ACCOUNT CE filter even when the target is the payer account.
	ScopeAccountOnly bool
}

// CredentialsAccountID returns the account ID whose credentials are in AWSConfig.
func (t AccountTarget) CredentialsAccountID() string {
	if id := strings.TrimSpace(t.PayerAccountID); id != "" {
		return id
	}
	return strings.TrimSpace(t.AccountID)
}

// ScopeToAccount reports whether Cost Explorer should filter to AccountID only.
func (t AccountTarget) ScopeToAccount() bool {
	return t.IsLinked() || t.ScopeAccountOnly
}

// IsLinked reports whether costs are scoped to a linked (member) account.
func (t AccountTarget) IsLinked() bool {
	payer := strings.TrimSpace(t.PayerAccountID)
	return payer != "" && payer != strings.TrimSpace(t.AccountID)
}

// DailyCostItem is net amortized cost for one calendar day.
type DailyCostItem struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
}

// CostBreakdownItem is one row when costs are split by service or linked account.
type CostBreakdownItem struct {
	Service     string  `json:"service,omitempty"`
	Account     string  `json:"account,omitempty"`
	AccountName string  `json:"account_name,omitempty"`
	Amount      float64 `json:"amount"`
}

// Label returns the merge/group key for this breakdown row (always the raw dimension value).
func (b CostBreakdownItem) Label(splitBy SplitBy) string {
	switch splitBy {
	case SplitByAccount:
		return b.Account
	default:
		return b.Service
	}
}

// DisplayLabel returns the formatted label for output (includes account ID when a name is known).
func (b CostBreakdownItem) DisplayLabel(splitBy SplitBy) string {
	switch splitBy {
	case SplitByAccount:
		if name := strings.TrimSpace(b.AccountName); name != "" && name != b.Account {
			return name + " (" + b.Account + ")"
		}
		return b.Label(splitBy)
	default:
		return b.Label(splitBy)
	}
}

// CostResult is the aggregated cost summary returned to callers.
type CostResult struct {
	Provider    Provider            `json:"provider"`
	AccountName string              `json:"account_name"`
	AccountID   string              `json:"account_id"`
	Metric      string              `json:"metric"`
	SplitBy     SplitBy             `json:"split_by,omitempty"`
	StartDate   string              `json:"start_date"`
	EndDate     string              `json:"end_date"`
	Amount      float64             `json:"amount"`
	Currency    string              `json:"currency"`
	Breakdown   []CostBreakdownItem `json:"breakdown,omitempty"`
	// Linked is true when costs are scoped to linked (member) accounts rather than payers.
	Linked bool `json:"linked,omitempty"`
}

// EmptyResult is a zero-amount summary for a period when no accounts were selected.
func EmptyResult(provider Provider, dr DateRange, splitBy SplitBy) CostResult {
	endInclusive := dr.End.AddDate(0, 0, -1)
	return CostResult{
		Provider:  provider,
		Metric:    MetricNetAmortized,
		SplitBy:   splitBy,
		StartDate: formatDate(dr.Start),
		EndDate:   formatDate(endInclusive),
	}
}

// Fetch retrieves cost data for one or more accounts and returns a combined summary.
func Fetch(ctx context.Context, q CostQuery) (CostResult, error) {
	if len(q.Accounts) == 0 {
		return CostResult{}, errors.New("at least one account is required")
	}
	targets := FilterOverlappingTargets(q.Accounts)

	if _, ok := planBulkFetch(targets); ok {
		reportBulkFetchProgress(q.Progress, len(targets), q.SplitBy)
		switch q.Provider {
		case ProviderAWS, "":
			opts := fetchAWSOptions{Now: time.Now()}
			if q.AWSFetch != nil {
				opts.ListAccountNames = q.AWSFetch.ListAccountNames
				opts.ResolveAccountNames = q.AWSFetch.ResolveAccountNames
			}
			return fetchAWSNetAmortizedBulk(ctx, q, targets, opts)
		case ProviderGCP:
			return CostResult{}, fmt.Errorf("%w: gcp", errProviderNotImplemented)
		default:
			return CostResult{}, fmt.Errorf("unknown provider %q", q.Provider)
		}
	}

	results := make([]CostResult, 0, len(targets))
	for i, acct := range targets {
		reportFetchProgress(q.Progress, acct, i+1, len(targets), q.SplitBy)
		single := q
		single.Accounts = []AccountTarget{acct}

		var r CostResult
		var err error
		switch q.Provider {
		case ProviderAWS, "":
			r, err = fetchAWSNetAmortized(ctx, single)
		case ProviderGCP:
			err = fmt.Errorf("%w: gcp", errProviderNotImplemented)
		default:
			err = fmt.Errorf("unknown provider %q", q.Provider)
		}
		if err != nil {
			return CostResult{}, fmt.Errorf("%s: %w", acct.AccountID, err)
		}
		results = append(results, r)
	}
	return MergeResults(results)
}

// FetchDaily retrieves per-day net amortized costs for one or more accounts.
func FetchDaily(ctx context.Context, q CostQuery) ([]DailyCostItem, string, error) {
	if len(q.Accounts) == 0 {
		return nil, "", errors.New("at least one account is required")
	}
	switch q.Provider {
	case ProviderAWS, "":
		targets := FilterOverlappingTargets(q.Accounts)
		if _, ok := planBulkFetch(targets); ok {
			reportBulkFetchProgress(q.Progress, len(targets), SplitByNone)
			opts := fetchAWSOptions{Now: time.Now()}
			return fetchAWSDailyNetAmortizedBulk(ctx, q, targets, opts)
		}
		series := make([][]DailyCostItem, 0, len(targets))
		var currency string
		for i, acct := range targets {
			reportFetchProgress(q.Progress, acct, i+1, len(targets), SplitByNone)
			single := q
			single.Accounts = []AccountTarget{acct}
			daily, cur, err := fetchAWSDailyNetAmortized(ctx, single)
			if err != nil {
				return nil, "", fmt.Errorf("%s: %w", acct.AccountID, err)
			}
			if currency == "" {
				currency = cur
			} else if cur != currency {
				return nil, "", fmt.Errorf("cannot merge accounts with different currencies (%s vs %s)", currency, cur)
			}
			series = append(series, daily)
		}
		return MergeDaily(series), currency, nil
	case ProviderGCP:
		return nil, "", fmt.Errorf("%w: gcp", errProviderNotImplemented)
	default:
		return nil, "", fmt.Errorf("unknown provider %q", q.Provider)
	}
}

func reportBulkFetchProgress(progress FetchProgress, accountCount int, splitBy SplitBy) {
	if progress == nil || accountCount <= 1 {
		return
	}
	switch splitBy {
	case SplitByService:
		progress.Step(fmt.Sprintf("Fetching costs by service for %d account(s) in batched Cost Explorer queries…", accountCount))
	default:
		progress.Step(fmt.Sprintf("Fetching costs for %d account(s) in one bulk Cost Explorer query…", accountCount))
	}
}

func reportFetchProgress(progress FetchProgress, acct AccountTarget, index, total int, splitBy SplitBy) {
	if progress == nil || total <= 1 || !shouldReportFetchProgress(index, total) {
		return
	}
	label := targetProgressLabel(acct)
	switch splitBy {
	case SplitByService:
		progress.Step(fmt.Sprintf("Fetching costs by service for %s [%d/%d]…", label, index, total))
	case SplitByAccount:
		progress.Step(fmt.Sprintf("Fetching costs for %s [%d/%d]…", label, index, total))
	default:
		progress.Step(fmt.Sprintf("Fetching costs for %s [%d/%d]…", label, index, total))
	}
}

func shouldReportFetchProgress(index, total int) bool {
	if total <= 1 {
		return false
	}
	if index == 1 || index == total {
		return true
	}
	if total <= 10 {
		return true
	}
	return index%25 == 0
}

func targetProgressLabel(acct AccountTarget) string {
	if name := strings.TrimSpace(acct.DisplayName); name != "" {
		return fmt.Sprintf("%s (%s)", name, acct.AccountID)
	}
	if alias := strings.TrimSpace(acct.DisplayAlias); alias != "" {
		return fmt.Sprintf("%s (%s)", alias, acct.AccountID)
	}
	return acct.AccountID
}
