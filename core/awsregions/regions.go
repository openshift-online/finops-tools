// Package awsregions provides region selection shared by AWS resource scans.
package awsregions

import (
	"sort"
	"strings"
)

// Normalize trims, deduplicates, and sorts region names without modifying input.
func Normalize(regions []string) []string {
	seen := make(map[string]bool, len(regions))
	var out []string
	for _, region := range regions {
		region = strings.TrimSpace(region)
		if region != "" && !seen[region] {
			seen[region] = true
			out = append(out, region)
		}
	}
	sort.Strings(out)
	return out
}

// Exclude removes skipped regions from an already selected or discovered list.
// An empty result must remain empty, rather than triggering region discovery.
func Exclude(regions, skip []string) []string {
	if len(skip) == 0 {
		return regions
	}
	excluded := make(map[string]bool, len(skip))
	for _, region := range Normalize(skip) {
		excluded[region] = true
	}
	out := make([]string, 0, len(regions))
	for _, region := range regions {
		if !excluded[strings.TrimSpace(region)] {
			out = append(out, region)
		}
	}
	return out
}
