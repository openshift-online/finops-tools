package snapshot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
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
		mu          sync.Mutex
		records     []Record
		warnings    []RegionWarning
		rdsContexts []RDSRegionContext
		ebsRunRate  float64
	)
	for i, target := range q.Targets {
		accountID := strings.TrimSpace(target.AccountID)
		if accountID == "" {
			return Result{}, fmt.Errorf("account target %d: account ID is required", i+1)
		}
		q.reportProgress(fmt.Sprintf("Scanning account %s (%d/%d)…", accountID, i+1, len(q.Targets)))

		regions, err := q.regionLister.ListEnabledRegions(ctx, target.AWSConfig, q.Regions)
		if err != nil {
			return Result{}, fmt.Errorf("%s: list regions: %w", accountID, err)
		}

		accountRecords, accountRDSContexts, accountEBSRunRate, regionWarnings, err := scanAccountRegions(ctx, q, target, accountID, regions, cutoff, typeSet)
		if err != nil {
			return Result{}, err
		}
		mu.Lock()
		records = append(records, accountRecords...)
		warnings = append(warnings, regionWarnings...)
		rdsContexts = append(rdsContexts, accountRDSContexts...)
		ebsRunRate += accountEBSRunRate
		mu.Unlock()
	}

	sortRecords(records)
	summary := buildSummary(records, int(q.OlderThan/(24*time.Hour)), rdsContexts, ebsRunRate)
	summary.SkippedRegions = sortRegionWarnings(warnings)
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

func (q Query) reportProgress(message string) {
	if q.Progress != nil {
		q.Progress(message)
	}
}

func kindSet(types []Kind) map[Kind]struct{} {
	set := make(map[Kind]struct{}, len(types))
	for _, k := range types {
		set[k] = struct{}{}
	}
	return set
}

func scanAccountRegions(
	ctx context.Context,
	q Query,
	target AccountTarget,
	accountID string,
	regions []string,
	cutoff time.Time,
	typeSet map[Kind]struct{},
) ([]Record, []RDSRegionContext, float64, []RegionWarning, error) {
	sem := make(chan struct{}, defaultRegionConcurrency)
	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		records     []Record
		rdsContexts []RDSRegionContext
		ebsRunRate  float64
		warnings    []RegionWarning
		scanErr     error
	)
	for _, region := range regions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			q.reportProgress(fmt.Sprintf("Scanning account %s, region %s…", accountID, region))
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
				warnings = append(warnings, warning)
				q.reportProgress(fmt.Sprintf("Skipping account %s, region %s: %s", accountID, region, warning.Message))
				return
			}
			records = append(records, regionRecords...)
			if regionRDSContext != nil {
				rdsContexts = append(rdsContexts, *regionRDSContext)
			}
			ebsRunRate += regionEBSRunRate
		}(region)
	}
	wg.Wait()

	if scanErr != nil {
		return records, rdsContexts, ebsRunRate, warnings, scanErr
	}
	if len(regions) > 0 && len(warnings) == len(regions) {
		return records, rdsContexts, ebsRunRate, warnings, fmt.Errorf("%s: all %d region(s) failed; first: %s", accountID, len(regions), warnings[0].Message)
	}
	return records, rdsContexts, ebsRunRate, warnings, nil
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

func scanRegion(
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
		if len(rdsRecords) > 0 {
			ctxData, err := q.rdsLister.GetRDSRegionContext(ctx, target.AWSConfig, region)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, nil, 0, err
				}
				ApplyRDSLegacyCosts(rdsRecords)
			} else {
				regionRDSContext = &ctxData
				ApplyRDSRegionalCosts(rdsRecords, ctxData)
			}
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
		CostDisclaimer:                      "Attributed costs apply to listed snapshots only. Per-snapshot $/MO is a proportional share of billed storage when Cost Explorer data is available; — on EBS means no incremental blocks. Account-wide billed snapshot storage is in JSON (summary.billed_costs).",
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
