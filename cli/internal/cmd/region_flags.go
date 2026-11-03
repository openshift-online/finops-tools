package cmd

import (
	"fmt"

	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/spf13/cobra"
)

func bindSkipRegionsFlag(cmd *cobra.Command, regions *[]string) {
	cmd.Flags().StringSliceVar(regions, "skip-regions", nil,
		"Exclude AWS regions from regional resource scans (comma-separated or repeatable; overrides --regions; global lookups still run)")
}

func skipRegionsForCommand(cmd *cobra.Command, regions []string) ([]string, error) {
	regions = awsregions.Normalize(regions)
	if cmd.Flags().Changed("skip-regions") && len(regions) == 0 {
		return nil, fmt.Errorf("--skip-regions requires at least one region")
	}
	return regions, nil
}
