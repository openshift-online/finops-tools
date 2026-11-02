package snapshot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/openshift-online/finops-tools/core/parallel"
)

const defaultRegionConcurrency = 5

// Fetch discovers old snapshots across accounts and regions.
func Fetch(ctx context.Context, q Query) (Result, error) {
	q = q.withDefaults()
	if len(q.Targets) == 0 {
		return Result{}, fmt.Errorf("at least one account target is required")
	}
	if q.OlderThan <= 0 {
		return Result{}, fmt.Errorf("older-than duration must be positive")
	}
	if len(q.Types) == 0 {
		return Result{}, fmt.Errorf("at least one snapshot type is required")
	}

	now := q.Now.UTC()
	cutoff := now.Add(-q.OlderThan)
	typeSet := kindSet(q.Types)

	var (
		mu              sync.Mutex
		records         []Record
		warnings        []RegionWarning
		skippedAccounts []AccountWarning
		rdsContexts     []RDSRegionContext
		ebsRunRate      float64
	)
	scopes := make([]AccountScanScope, len(q.Targets))
	err := parallel.ForEach(ctx, q.Workers, len(q.Targets), func(ctx context.Context, i int) error {
		target := q.Targets[i]
		accountID := strings.TrimSpace(target.AccountID)
		if accountID == "" {
			return fmt.Errorf("account target %d: account ID is required", i+1)
		}
		scopes[i] = AccountScanScope{AccountID: accountID, Regions: []string{}}

		scanTarget, err := target.withFreshConfig(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			mu.Lock()
			skippedAccounts = append(skippedAccounts, AccountWarning{
				AccountID:    accountID,
				DisplayAlias: target.DisplayAlias,
				Message:      regionErrorMessage(err),
			})
			mu.Unlock()
			q.advanceAccountProgress()
			return nil
		}

		scan, accountSkip, err := scanAccountRegions(ctx, q, scanTarget, accountID, cutoff, typeSet)
		if err != nil {
			return err
		}
		scopes[i].Regions = scan.regions
		for _, rds := range scan.rdsContexts {
			if rds.Unavailable {
				scopes[i].RDSCostEstimationIncomplete = true
			}
		}
		mu.Lock()
		records = append(records, scan.records...)
		warnings = append(warnings, scan.warnings...)
		rdsContexts = append(rdsContexts, scan.rdsContexts...)
		ebsRunRate += scan.ebsRunRate
		if accountSkip != nil {
			skippedAccounts = append(skippedAccounts, *accountSkip)
		}
		mu.Unlock()
		q.advanceAccountProgress()
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	sortRecords(records)
	summary := buildSummary(records, int(q.OlderThan/(24*time.Hour)), rdsContexts, ebsRunRate)
	summary.SkippedRegions = sortRegionWarnings(warnings)
	summary.SkippedAccounts = sortAccountWarnings(skippedAccounts)
	summary.ExcludedRegions = awsregions.Normalize(q.SkipRegions)
	summary.ScanScopes = scopes
	return Result{Records: records, Summary: summary}, nil
}

func (q Query) withDefaults() Query {
	if q.Now.IsZero() {
		q.Now = time.Now().UTC()
	}
	if q.regionLister == nil {
		q.regionLister = ec2RegionLister{}
	}
	if q.ebsLister == nil {
		q.ebsLister = ec2EBSLister{}
	}
	if q.rdsLister == nil {
		q.rdsLister = awsRDSLister{}
	}
	return q
}

func (q Query) advanceAccountProgress() {
	if q.AccountProgress != nil {
		q.AccountProgress.Advance()
	}
}

func (t AccountTarget) withFreshConfig(ctx context.Context) (AccountTarget, error) {
	if t.ConfigLoader == nil {
		return t, nil
	}
	cfg, err := t.ConfigLoader(ctx)
	if err != nil {
		return AccountTarget{}, err
	}
	t.AWSConfig = cfg
	return t, nil
}

func kindSet(types []Kind) map[Kind]struct{} {
	set := make(map[Kind]struct{}, len(types))
	for _, k := range types {
		set[k] = struct{}{}
	}
	return set
}

type accountScanResult struct {
	records     []Record
	rdsContexts []RDSRegionContext
	ebsRunRate  float64
	warnings    []RegionWarning
	regions     []string
}

func scanAccountRegions(
	ctx context.Context,
	q Query,
	target AccountTarget,
	accountID string,
	cutoff time.Time,
	typeSet map[Kind]struct{},
) (accountScanResult, *AccountWarning, error) {
	// Discover enabled regions per account so opt-in differences are not missed
	// when sharing a single list from an arbitrary target.
	regions, err := q.listEnabledRegions(ctx, &target)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return accountScanResult{}, nil, err
		}
		// Keep multi-account scans going when one member has bad/expired creds
		// or cannot call DescribeRegions.
		skip := &AccountWarning{
			AccountID:    accountID,
			DisplayAlias: target.DisplayAlias,
			Message:      regionErrorMessage(err),
		}
		return accountScanResult{regions: []string{}}, skip, nil
	}

	sem := make(chan struct{}, defaultRegionConcurrency)
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		scanErr error
	)
	result := accountScanResult{regions: []string{}}
	for _, region := range regions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			regionRecords, regionRDSContext, regionEBSRunRate, err := scanRegion(ctx, q, target, accountID, region, cutoff, typeSet)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if errors.Is(err, context.Canceled) {
					scanErr = err
					return
				}
				warning := RegionWarning{
					AccountID: accountID,
					Region:    region,
					Message:   regionErrorMessage(err),
				}
				result.warnings = append(result.warnings, warning)
				return
			}
			result.regions = append(result.regions, region)
			result.records = append(result.records, regionRecords...)
			if regionRDSContext != nil {
				result.rdsContexts = append(result.rdsContexts, *regionRDSContext)
			}
			result.ebsRunRate += regionEBSRunRate
		}(region)
	}
	wg.Wait()

	if scanErr != nil {
		return result, nil, scanErr
	}
	sort.Strings(result.regions)
	result.warnings = collapseRegionWarnings(accountID, regions, result.warnings)
	return result, nil, nil
}

func (q Query) listEnabledRegions(ctx context.Context, target *AccountTarget) ([]string, error) {
	regions, err := q.regionLister.ListEnabledRegions(ctx, target.AWSConfig, q.Regions)
	if err != nil && isExpiredCredentialError(err) && target.ConfigLoader != nil {
		cfg, refreshErr := target.ConfigLoader(ctx)
		if refreshErr != nil {
			return nil, err
		}
		target.AWSConfig = cfg
		regions, err = q.regionLister.ListEnabledRegions(ctx, target.AWSConfig, q.Regions)
	}
	if err != nil {
		return nil, err
	}
	return awsregions.Exclude(regions, q.SkipRegions), nil
}

func sortRegionWarnings(warnings []RegionWarning) []RegionWarning {
	if len(warnings) == 0 {
		return nil
	}
	out := append([]RegionWarning(nil), warnings...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].AccountID != out[j].AccountID {
			return out[i].AccountID < out[j].AccountID
		}
		if out[i].Region != out[j].Region {
			return out[i].Region < out[j].Region
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func sortAccountWarnings(warnings []AccountWarning) []AccountWarning {
	if len(warnings) == 0 {
		return nil
	}
	out := append([]AccountWarning(nil), warnings...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].AccountID != out[j].AccountID {
			return out[i].AccountID < out[j].AccountID
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func scanRegion(
	ctx context.Context,
	q Query,
	target AccountTarget,
	accountID, region string,
	cutoff time.Time,
	typeSet map[Kind]struct{},
) ([]Record, *RDSRegionContext, float64, error) {
	records, regionRDSContext, ebsRunRate, err := scanRegionOnce(ctx, q, target, accountID, region, cutoff, typeSet)
	if err != nil && isExpiredCredentialError(err) && target.ConfigLoader != nil {
		cfg, refreshErr := target.ConfigLoader(ctx)
		if refreshErr != nil {
			return nil, nil, 0, err
		}
		target.AWSConfig = cfg
		return scanRegionOnce(ctx, q, target, accountID, region, cutoff, typeSet)
	}
	return records, regionRDSContext, ebsRunRate, err
}

func scanRegionOnce(
	ctx context.Context,
	q Query,
	target AccountTarget,
	accountID, region string,
	cutoff time.Time,
	typeSet map[Kind]struct{},
) ([]Record, *RDSRegionContext, float64, error) {
	var records []Record
	var regionRDSContext *RDSRegionContext
	var ebsRunRate float64
	if _, ok := typeSet[KindEBSSnapshot]; ok {
		ebsRecords, regionalRunRate, err := q.ebsLister.ListEBSSnapshots(ctx, target.AWSConfig, region, accountID, cutoff, q.MinSizeGiB)
		if err != nil {
			return nil, nil, 0, err
		}
		records = append(records, ebsRecords...)
		ebsRunRate += regionalRunRate
	}
	if typeSetHasRDS(typeSet) {
		rdsRecords, err := q.rdsLister.ListRDSSnapshots(ctx, target.AWSConfig, region, accountID, cutoff, q.MinSizeGiB)
		if err != nil {
			return nil, nil, 0, err
		}
		rdsRecords = filterRDSRecords(rdsRecords, typeSet)
		// Even regions without old snapshots contribute to billed backup storage
		// and must be represented in the attribution denominator.
		ctxData, err := q.rdsLister.GetRDSRegionContext(ctx, target.AWSConfig, region)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, nil, 0, err
			}
			regionRDSContext = &RDSRegionContext{Unavailable: true}
			ApplyRDSLegacyCosts(rdsRecords)
		} else {
			regionRDSContext = &ctxData
			ApplyRDSRegionalCosts(rdsRecords, ctxData)
		}
		records = append(records, rdsRecords...)
	}
	return records, regionRDSContext, ebsRunRate, nil
}

func typeSetHasRDS(typeSet map[Kind]struct{}) bool {
	_, okInstance := typeSet[KindRDSSnapshot]
	_, okCluster := typeSet[KindRDSClusterSnapshot]
	return okInstance || okCluster
}

func filterRDSRecords(records []Record, typeSet map[Kind]struct{}) []Record {
	out := make([]Record, 0, len(records))
	for _, rec := range records {
		if _, ok := typeSet[rec.Kind]; ok {
			out = append(out, rec)
		}
	}
	return out
}

func sortRecords(records []Record) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].EstimatedMonthlyCostUSD != records[j].EstimatedMonthlyCostUSD {
			return records[i].EstimatedMonthlyCostUSD > records[j].EstimatedMonthlyCostUSD
		}
		if records[i].AgeDays != records[j].AgeDays {
			return records[i].AgeDays > records[j].AgeDays
		}
		return records[i].ResourceID < records[j].ResourceID
	})
}

func buildSummary(records []Record, olderThanDays int, rdsContexts []RDSRegionContext, ebsRunRateUSD float64) Summary {
	byKind := make(map[Kind]*KindSummary)
	byAccount := make(map[string]*AccountSummary)
	var totalCost float64
	for _, rec := range records {
		totalCost += rec.EstimatedMonthlyCostUSD
		ks := byKind[rec.Kind]
		if ks == nil {
			ks = &KindSummary{Kind: rec.Kind}
			byKind[rec.Kind] = ks
		}
		ks.Count++
		ks.EstimatedMonthlyCostUSD += rec.EstimatedMonthlyCostUSD

		as := byAccount[rec.AccountID]
		if as == nil {
			as = &AccountSummary{AccountID: rec.AccountID}
			byAccount[rec.AccountID] = as
		}
		as.Count++
		as.EstimatedMonthlyCostUSD += rec.EstimatedMonthlyCostUSD
	}

	kindSummaries := make([]KindSummary, 0, len(byKind))
	for _, ks := range byKind {
		kindSummaries = append(kindSummaries, *ks)
	}
	sort.Slice(kindSummaries, func(i, j int) bool {
		if kindSummaries[i].EstimatedMonthlyCostUSD != kindSummaries[j].EstimatedMonthlyCostUSD {
			return kindSummaries[i].EstimatedMonthlyCostUSD > kindSummaries[j].EstimatedMonthlyCostUSD
		}
		return kindSummaries[i].Kind < kindSummaries[j].Kind
	})

	accountSummaries := make([]AccountSummary, 0, len(byAccount))
	for _, as := range byAccount {
		accountSummaries = append(accountSummaries, *as)
	}
	sort.Slice(accountSummaries, func(i, j int) bool {
		if accountSummaries[i].EstimatedMonthlyCostUSD != accountSummaries[j].EstimatedMonthlyCostUSD {
			return accountSummaries[i].EstimatedMonthlyCostUSD > accountSummaries[j].EstimatedMonthlyCostUSD
		}
		return accountSummaries[i].AccountID < accountSummaries[j].AccountID
	})

	var rdsExcessGiB float64
	var rdsRunRateUSD float64
	for _, ctx := range rdsContexts {
		excess := ctx.TotalBackupGiB - ctx.FreePoolGiB
		if excess < 0 {
			excess = 0
		}
		rdsExcessGiB += excess
		rdsRunRateUSD += RDSMonthlyBackupRunRateUSD(excess)
	}

	return Summary{
		TotalCount:                          len(records),
		EstimatedMonthlyCostUSD:             totalCost,
		RDSBackupRegionalExcessGiB:          rdsExcessGiB,
		RDSBackupEstimatedMonthlyRunRateUSD: rdsRunRateUSD,
		EBSEstimatedMonthlyRunRateUSD:       ebsRunRateUSD,
		OlderThanDays:                       olderThanDays,
		ByKind:                              kindSummaries,
		ByAccount:                           accountSummaries,
		CostDisclaimer:                      "Attributed costs apply to listed snapshots only, using billed storage for the successfully scanned regions. Account-wide billed costs are shown for comparison. Where matching billing or run rates are unavailable, costs remain estimates; — on EBS means no incremental blocks.",
	}
}

// ParseRegions parses a comma-separated --regions flag.
func ParseRegions(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	return splitCommaList(s)
}
