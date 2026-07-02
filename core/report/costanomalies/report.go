// Package costanomalies fetches cost anomalies detected by AWS Cost Anomaly Detection.
package costanomalies

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/openshift-online/finops-tools/core/apilog"
	"github.com/openshift-online/finops-tools/core/cost"
)

const costExplorerRegion = "us-east-1"

// CostAnomaliesAPI is the subset of the CE client used for anomaly fetch (mockable).
type CostAnomaliesAPI interface {
	GetAnomalies(
		ctx context.Context,
		params *costexplorer.GetAnomaliesInput,
		optFns ...func(*costexplorer.Options),
	) (*costexplorer.GetAnomaliesOutput, error)
}

// RootCause is one root-cause attribution for an anomaly.
type RootCause struct {
	Account      string  // linked account ID
	AccountName  string
	Region       string
	Service      string
	UsageType    string
	Contribution float64 // dollar contribution to the anomaly
}

// Anomaly is one detected cost anomaly from AWS Cost Anomaly Detection.
type Anomaly struct {
	ID             string
	StartDate      string  // YYYY-MM-DD
	EndDate        string  // YYYY-MM-DD; empty if still open
	Service        string  // DimensionValue (service or account dimension)
	CurrentScore   float64 // anomaly score at time of report
	MaxScore       float64
	TotalImpact    float64 // actual − expected spend (dollars)
	ActualSpend    float64
	ExpectedSpend  float64
	ImpactPct      float64 // (TotalImpact / ExpectedSpend) × 100
	RootCauses     []RootCause
}

// Report holds all anomalies in the requested date range.
type Report struct {
	GeneratedAt time.Time
	StartDate   string
	EndDate     string
	Anomalies   []Anomaly
}

// Build fetches cost anomalies from AWS Cost Anomaly Detection for the given
// accounts and date range. When multiple linked accounts or OUs are selected,
// anomalies are limited to those accounts. A single unscoped payer target
// returns organization-wide anomalies. Results are sorted by total dollar
// impact descending.
func Build(ctx context.Context, accounts []cost.AccountTarget, dr cost.DateRange) (Report, error) {
	return buildWith(ctx, defaultCEClientFactory, accounts, dr)
}

type ceClientFactory func(cfg aws.Config) CostAnomaliesAPI

func defaultCEClientFactory(cfg aws.Config) CostAnomaliesAPI {
	region := cfg.Region
	if region == "" {
		region = costExplorerRegion
	}
	cfg.Region = region
	inner := costexplorer.NewFromConfig(cfg, func(o *costexplorer.Options) {
		o.Region = costExplorerRegion
	})
	return apilog.WrapCostAnomalies(inner)
}

// buildWith is the testable core: accepts any CostAnomaliesAPI factory.
func buildWith(ctx context.Context, newClient ceClientFactory, accounts []cost.AccountTarget, dr cost.DateRange) (Report, error) {
	if len(accounts) == 0 {
		return Report{}, fmt.Errorf("at least one account required")
	}

	ceClients := make(map[string]CostAnomaliesAPI)
	seenAnomalies := make(map[string]struct{})
	all := make([]Anomaly, 0)

	for _, group := range groupAccountsByCredentials(accounts) {
		credID := group[0].CredentialsAccountID()
		ce, ok := ceClients[credID]
		if !ok {
			ce = newClient(group[0].AWSConfig)
			ceClients[credID] = ce
		}

		raw, err := fetchAll(ctx, ce, dr)
		if err != nil {
			return Report{}, fmt.Errorf("%s: %w", accountDisplayName(group[0]), err)
		}

		anomalies := parseAnomalies(raw)
		anomalies = filterAnomaliesForAccounts(anomalies, accountFilterSet(group))

		for _, a := range anomalies {
			if _, ok := seenAnomalies[a.ID]; ok {
				continue
			}
			seenAnomalies[a.ID] = struct{}{}
			all = append(all, a)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].TotalImpact > all[j].TotalImpact
	})

	return Report{
		GeneratedAt: time.Now().UTC(),
		StartDate:   dr.Start.Format("2006-01-02"),
		EndDate:     dr.End.AddDate(0, 0, -1).Format("2006-01-02"),
		Anomalies:   all,
	}, nil
}

func groupAccountsByCredentials(accounts []cost.AccountTarget) [][]cost.AccountTarget {
	order := make([]string, 0)
	groups := make(map[string][]cost.AccountTarget)
	for _, acct := range accounts {
		credID := acct.CredentialsAccountID()
		if _, ok := groups[credID]; !ok {
			order = append(order, credID)
		}
		groups[credID] = append(groups[credID], acct)
	}
	out := make([][]cost.AccountTarget, 0, len(order))
	for _, credID := range order {
		out = append(out, groups[credID])
	}
	return out
}

// accountFilterSet returns selected account IDs when the caller narrowed scope
// (multiple targets or a single scoped/linked account). A lone unscoped payer
// returns nil so organization-wide anomalies are kept.
func accountFilterSet(accounts []cost.AccountTarget) map[string]struct{} {
	if len(accounts) == 1 && !accounts[0].ScopeToAccount() {
		return nil
	}
	set := make(map[string]struct{}, len(accounts))
	for _, acct := range accounts {
		if id := strings.TrimSpace(acct.AccountID); id != "" {
			set[id] = struct{}{}
		}
	}
	return set
}

func filterAnomaliesForAccounts(anomalies []Anomaly, accountIDs map[string]struct{}) []Anomaly {
	if len(accountIDs) == 0 {
		return anomalies
	}

	filtered := make([]Anomaly, 0, len(anomalies))
	for _, a := range anomalies {
		if len(a.RootCauses) == 0 {
			continue
		}
		causes := make([]RootCause, 0, len(a.RootCauses))
		var totalImpact float64
		for _, rc := range a.RootCauses {
			if _, ok := accountIDs[rc.Account]; ok {
				causes = append(causes, rc)
				totalImpact += rc.Contribution
			}
		}
		if len(causes) == 0 {
			continue
		}
		a.RootCauses = causes
		a.TotalImpact = totalImpact
		if a.Service == "" {
			a.Service = causes[0].Service
		}
		filtered = append(filtered, a)
	}
	return filtered
}

func accountDisplayName(acct cost.AccountTarget) string {
	if name := strings.TrimSpace(acct.DisplayName); name != "" {
		return name
	}
	if alias := strings.TrimSpace(acct.DisplayAlias); alias != "" {
		return alias
	}
	return strings.TrimSpace(acct.AccountID)
}

// fetchAll paginates through GetAnomalies until all results are collected.
func fetchAll(ctx context.Context, ce CostAnomaliesAPI, dr cost.DateRange) ([]types.Anomaly, error) {
	input := &costexplorer.GetAnomaliesInput{
		DateInterval: &types.AnomalyDateInterval{
			StartDate: aws.String(dr.Start.Format("2006-01-02")),
			EndDate:   aws.String(dr.End.AddDate(0, 0, -1).Format("2006-01-02")),
		},
	}

	var all []types.Anomaly
	for {
		resp, err := ce.GetAnomalies(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("fetch anomalies: %w", err)
		}
		all = append(all, resp.Anomalies...)

		if resp.NextPageToken == nil || *resp.NextPageToken == "" {
			break
		}
		input.NextPageToken = resp.NextPageToken
	}

	return all, nil
}

// parseAnomalies converts AWS Anomaly slice into the Report's Anomaly slice,
// sorted by TotalImpact descending.
func parseAnomalies(raw []types.Anomaly) []Anomaly {
	result := make([]Anomaly, 0, len(raw))
	for _, a := range raw {
		if a.AnomalyId == nil || a.AnomalyScore == nil || a.Impact == nil {
			continue
		}

		anomaly := Anomaly{
			ID:           *a.AnomalyId,
			Service:      aws.ToString(a.DimensionValue),
			CurrentScore: a.AnomalyScore.CurrentScore,
			MaxScore:     a.AnomalyScore.MaxScore,
			TotalImpact:  a.Impact.TotalImpact,
			RootCauses:   parseRootCauses(a.RootCauses),
		}

		if a.AnomalyStartDate != nil {
			anomaly.StartDate = *a.AnomalyStartDate
		}
		if a.AnomalyEndDate != nil {
			anomaly.EndDate = *a.AnomalyEndDate
		}
		if a.Impact.TotalActualSpend != nil {
			anomaly.ActualSpend = *a.Impact.TotalActualSpend
		}
		if a.Impact.TotalExpectedSpend != nil {
			anomaly.ExpectedSpend = *a.Impact.TotalExpectedSpend
		}
		if a.Impact.TotalImpactPercentage != nil {
			anomaly.ImpactPct = *a.Impact.TotalImpactPercentage
		}

		// Derive service label from root causes when DimensionValue is empty.
		if anomaly.Service == "" && len(anomaly.RootCauses) > 0 {
			anomaly.Service = anomaly.RootCauses[0].Service
		}

		result = append(result, anomaly)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalImpact > result[j].TotalImpact
	})

	return result
}

func parseRootCauses(raw []types.RootCause) []RootCause {
	causes := make([]RootCause, 0, len(raw))
	for _, r := range raw {
		rc := RootCause{
			Account:     aws.ToString(r.LinkedAccount),
			AccountName: aws.ToString(r.LinkedAccountName),
			Region:      aws.ToString(r.Region),
			Service:     aws.ToString(r.Service),
			UsageType:   aws.ToString(r.UsageType),
		}
		if r.Impact != nil {
			rc.Contribution = r.Impact.Contribution
		}
		causes = append(causes, rc)
	}
	return causes
}
