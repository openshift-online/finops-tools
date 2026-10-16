package extendedsupport

import (
	"context"
	"strings"
)

// RetrySkippedAccounts makes one scan attempt for targets in result's skipped
// account list. Other targets are ignored, and skipped accounts without a target
// are retained. Callers may supply fresh configuration loaders in q; services,
// regions, and the scan time should match the original query.
// The returned result replaces retried accounts' findings and warnings and
// rebuilds the summary. Region-only failures are not retried.
func RetrySkippedAccounts(ctx context.Context, q Query, result Result) (Result, error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	skipped := make(map[string]bool, len(result.Summary.SkippedAccounts))
	for _, warning := range result.Summary.SkippedAccounts {
		skipped[warning.AccountID] = true
	}
	retried := make(map[string]bool)
	var targets []AccountTarget
	for _, target := range q.Targets {
		id := strings.TrimSpace(target.AccountID)
		if skipped[id] && !retried[id] {
			targets = append(targets, target)
			retried[id] = true
		}
	}
	if len(targets) == 0 {
		return result, nil
	}
	q.Targets = targets
	retry, err := Fetch(ctx, q)
	if err != nil {
		return result, err
	}

	// Build new slices so merging does not mutate the original result.
	var records []Record
	for _, record := range result.Records {
		if !retried[record.AccountID] {
			records = append(records, record)
		}
	}
	var regions []RegionWarning
	for _, warning := range result.Summary.SkippedRegions {
		if !retried[warning.AccountID] {
			regions = append(regions, warning)
		}
	}
	var resources []ResourceWarning
	for _, warning := range result.Summary.UnclassifiedResources {
		if !retried[warning.AccountID] {
			resources = append(resources, warning)
		}
	}
	var accounts []AccountWarning
	for _, warning := range result.Summary.SkippedAccounts {
		if !retried[warning.AccountID] {
			accounts = append(accounts, warning)
		}
	}
	return buildResult(
		append(records, retry.Records...),
		append(regions, retry.Summary.SkippedRegions...),
		append(resources, retry.Summary.UnclassifiedResources...),
		append(accounts, retry.Summary.SkippedAccounts...),
	), nil
}
