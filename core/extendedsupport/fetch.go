package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openshift-online/finops-tools/core/parallel"
)

const defaultRegionConcurrency = 5

// Fetch discovers extended-support resources across accounts and regions.
// Per-account and per-region failures are recorded as warnings so one bad
// credential or opt-in region does not abort the rest of the scan.
func Fetch(ctx context.Context, q Query) (Result, error) {
	q = q.withDefaults()
	if len(q.Targets) == 0 {
		return Result{}, fmt.Errorf("at least one account target is required")
	}
	if len(q.Services) == 0 {
		return Result{}, fmt.Errorf("at least one service is required")
	}

	now := q.Now.UTC()
	serviceSet := serviceSet(q.Services)

	var (
		mu              sync.Mutex
		records         []Record
		warnings        []RegionWarning
		unclassified    []ResourceWarning
		skippedAccounts []AccountWarning
	)
	err := parallel.ForEach(ctx, q.Workers, len(q.Targets), func(ctx context.Context, i int) error {
		target := q.Targets[i]
		accountID := strings.TrimSpace(target.AccountID)
		if accountID == "" {
			return fmt.Errorf("account target %d: account ID is required", i+1)
		}

		scanTarget, err := target.withFreshConfig(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			mu.Lock()
			skippedAccounts = append(skippedAccounts, AccountWarning{
				AccountID:    accountID,
				DisplayAlias: target.DisplayAlias,
				Message:      accountWarningMessage(err, "loading account credentials"),
			})
			mu.Unlock()
			q.advanceAccountProgress()
			return nil
		}

		accountResult, accountSkip, err := scanAccountRegions(ctx, q, scanTarget, accountID, now, serviceSet)
		if err != nil {
			return err
		}
		mu.Lock()
		records = append(records, accountResult.Records...)
		warnings = append(warnings, accountResult.Warnings...)
		unclassified = append(unclassified, accountResult.UnclassifiedResources...)
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

	return buildResult(records, warnings, unclassified, skippedAccounts), nil
}

func buildResult(records []Record, warnings []RegionWarning, unclassified []ResourceWarning, skippedAccounts []AccountWarning) Result {
	sortRecords(records)
	return Result{
		Records: records,
		Summary: Summary{
			TotalCount:            len(records),
			ByService:             summarizeByService(records),
			ByAccount:             summarizeByAccount(records),
			SkippedRegions:        sortRegionWarnings(warnings),
			SkippedAccounts:       sortAccountWarnings(skippedAccounts),
			UnclassifiedResources: sortResourceWarnings(unclassified),
		},
	}
}

func (q Query) withDefaults() Query {
	if q.Now.IsZero() {
		q.Now = time.Now().UTC()
	}
	if q.regionLister == nil {
		q.regionLister = ec2RegionLister{}
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

func serviceSet(services []Service) map[Service]struct{} {
	set := make(map[Service]struct{}, len(services))
	for _, service := range services {
		set[service] = struct{}{}
	}
	return set
}

func scanAccountRegions(
	ctx context.Context,
	q Query,
	target AccountTarget,
	accountID string,
	now time.Time,
	services map[Service]struct{},
) (regionScanResult, *AccountWarning, error) {
	regions, err := q.regionLister.ListEnabledRegions(ctx, target.AWSConfig, q.Regions)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return regionScanResult{}, nil, err
		}
		skip := &AccountWarning{
			AccountID:    accountID,
			DisplayAlias: target.DisplayAlias,
			Message:      accountWarningMessage(err, "discovering enabled regions"),
		}
		return regionScanResult{}, skip, nil
	}

	sem := make(chan struct{}, defaultRegionConcurrency)
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		result  regionScanResult
		scanErr error
	)
	for _, region := range regions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			regionResult, err := scanRegion(ctx, q, target, accountID, region, now, services)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if errors.Is(err, context.Canceled) {
					scanErr = err
					return
				}
				result.Warnings = append(result.Warnings, RegionWarning{
					AccountID: accountID,
					Region:    region,
					Message:   warningMessage(err),
				})
			}
			result.Records = append(result.Records, regionResult.Records...)
			result.Warnings = append(result.Warnings, regionResult.Warnings...)
			result.UnclassifiedResources = append(result.UnclassifiedResources, regionResult.UnclassifiedResources...)
		}(region)
	}
	wg.Wait()
	if scanErr != nil {
		return regionScanResult{}, nil, scanErr
	}
	return result, nil, nil
}

func scanRegion(
	ctx context.Context,
	q Query,
	target AccountTarget,
	accountID, region string,
	now time.Time,
	services map[Service]struct{},
) (regionScanResult, error) {
	var result regionScanResult
	if _, ok := services[ServiceRDS]; ok {
		rdsResult, err := q.rdsLister.ListRDSExtendedSupport(ctx, target.AWSConfig, region, accountID, now)
		if err != nil {
			return rdsResult, err
		}
		result.Records = append(result.Records, rdsResult.Records...)
		result.Warnings = append(result.Warnings, rdsResult.Warnings...)
		result.UnclassifiedResources = append(result.UnclassifiedResources, rdsResult.UnclassifiedResources...)
	}
	return result, nil
}

func sortResourceWarnings(warnings []ResourceWarning) []ResourceWarning {
	if len(warnings) == 0 {
		return nil
	}
	sort.Slice(warnings, func(i, j int) bool {
		a, b := warnings[i], warnings[j]
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		if a.Region != b.Region {
			return a.Region < b.Region
		}
		if a.ResourceType != b.ResourceType {
			return a.ResourceType < b.ResourceType
		}
		if a.ResourceID != b.ResourceID {
			return a.ResourceID < b.ResourceID
		}
		return a.Message < b.Message
	})
	return warnings
}

func accountWarningMessage(err error, stage string) string {
	message := warningMessage(err)
	if strings.TrimSpace(message) == "" {
		return fmt.Sprintf("No error message was provided while %s", stage)
	}
	return message
}

func warningMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if idx := strings.LastIndex(msg, ": "); idx >= 0 {
		tail := strings.TrimSpace(msg[idx+2:])
		if tail != "" {
			return tail
		}
	}
	return msg
}

func sortRecords(records []Record) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].AccountID != records[j].AccountID {
			return records[i].AccountID < records[j].AccountID
		}
		if records[i].Region != records[j].Region {
			return records[i].Region < records[j].Region
		}
		if records[i].Service != records[j].Service {
			return records[i].Service < records[j].Service
		}
		if records[i].ResourceType != records[j].ResourceType {
			return records[i].ResourceType < records[j].ResourceType
		}
		return records[i].ResourceID < records[j].ResourceID
	})
}

func summarizeByService(records []Record) []ServiceSummary {
	byService := make(map[Service]*ServiceSummary)
	for _, rec := range records {
		summary := byService[rec.Service]
		if summary == nil {
			summary = &ServiceSummary{Service: rec.Service}
			byService[rec.Service] = summary
		}
		summary.Count++
	}
	out := make([]ServiceSummary, 0, len(byService))
	for _, summary := range byService {
		out = append(out, *summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Service < out[j].Service
	})
	return out
}

func summarizeByAccount(records []Record) []AccountSummary {
	byAccount := make(map[string]*AccountSummary)
	for _, rec := range records {
		summary := byAccount[rec.AccountID]
		if summary == nil {
			summary = &AccountSummary{AccountID: rec.AccountID}
			byAccount[rec.AccountID] = summary
		}
		summary.Count++
	}
	out := make([]AccountSummary, 0, len(byAccount))
	for _, summary := range byAccount {
		out = append(out, *summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].AccountID < out[j].AccountID
	})
	return out
}

func sortRegionWarnings(warnings []RegionWarning) []RegionWarning {
	if len(warnings) == 0 {
		return nil
	}
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].AccountID != warnings[j].AccountID {
			return warnings[i].AccountID < warnings[j].AccountID
		}
		if warnings[i].Region != warnings[j].Region {
			return warnings[i].Region < warnings[j].Region
		}
		return warnings[i].Message < warnings[j].Message
	})
	return warnings
}

func sortAccountWarnings(warnings []AccountWarning) []AccountWarning {
	if len(warnings) == 0 {
		return nil
	}
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].AccountID != warnings[j].AccountID {
			return warnings[i].AccountID < warnings[j].AccountID
		}
		return warnings[i].Message < warnings[j].Message
	})
	return warnings
}
