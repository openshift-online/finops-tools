// snapshot_list.go implements "finops snapshot list" to find stale EBS and RDS snapshots.
package cmd

import (
	"fmt"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	"github.com/openshift-online/finops-tools/core/snapshot"
	"github.com/spf13/cobra"
)

var (
	snapshotListAccount        string
	snapshotListAccountAliases string
	snapshotListFormat         string
	snapshotListMinSizeGiB     float64
	snapshotListOlderThanDays  int
	snapshotListOU             string
	snapshotListOUDirect       bool
	snapshotListPayer          string
	snapshotListQuiet          bool
	snapshotListRegions        string
	snapshotListRole           string
	snapshotListSkipOrgCache   bool
	snapshotListRefreshOrgCache bool
	snapshotListTagKey         string
	snapshotListTagValue       string
	snapshotListTypes          string
	snapshotListFetch          = snapshot.Fetch
)

var snapshotListCmd = &cobra.Command{
	Use:   "list",
	Short: "List EBS and RDS snapshots with estimated storage costs",
	Long: `Discover EBS and RDS snapshots older than a cutoff and estimate monthly storage cost.

Account selection matches finops cost get: --account, --account-alias, --ou, or --tag-key with --payer.
Linked member accounts are scanned using role assumption from the payer.

Cost estimates use incremental EBS snapshot chains where possible and RDS regional excess shares.
When Cost Explorer data is available, summary shows attributed storage cost for listed snapshots.
Per-snapshot $/MO allocates billed cost proportionally; — on EBS means no incremental blocks.
Account-wide billed snapshot storage is included in JSON output only.

Required IAM permissions in each scanned account:
  ec2:DescribeRegions, ec2:DescribeSnapshots
  rds:DescribeDBInstances, rds:DescribeDBClusters, rds:DescribeDBSnapshots, rds:DescribeDBClusterSnapshots

Payer credentials also need sts:AssumeRole into the configured linked-account role and
ce:GetCostAndUsage with LINKED_ACCOUNT scope for billed cost lines.

Examples:
  finops snapshot list --account-alias rh-control
  finops snapshot list --account-alias rh-control --older-than-days 365 --format json
  finops snapshot list --payer rh-control --tag-key organization
  finops snapshot list --ou ou-abcd-1234 --payer rh-control --types ebs`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		sel, err := parseCostTargetSelector(
			snapshotListAccount, snapshotListAccountAliases, snapshotListOU, snapshotListPayer,
			snapshotListTagKey, snapshotListTagValue, snapshotListOUDirect,
			snapshotListSkipOrgCache, snapshotListRefreshOrgCache,
		)
		if err != nil {
			return err
		}
		if _, err := validateCostTargetSelector(sel); err != nil {
			return err
		}
		if _, err := output.ParseFormat(snapshotListFormat); err != nil {
			return err
		}
		if snapshotListOlderThanDays <= 0 {
			return fmt.Errorf("--older-than-days must be positive")
		}
		if snapshotListMinSizeGiB < 0 {
			return fmt.Errorf("--min-size-gib must be >= 0")
		}
		if _, err := snapshot.ParseTypes(snapshotListTypes); err != nil {
			return err
		}
		if _, err := snapshot.ParseRegions(snapshotListRegions); err != nil {
			return err
		}
		return validateOrgCacheFlags(snapshotListSkipOrgCache, snapshotListRefreshOrgCache)
	},
	RunE: runSnapshotList,
}

func init() {
	snapshotCmd.AddCommand(snapshotListCmd)
	snapshotListCmd.Flags().StringVar(&snapshotListAccount, "account", "", "AWS account ID(s), comma-separated 12-digit IDs")
	snapshotListCmd.Flags().StringVar(&snapshotListAccountAliases, "account-alias", "", "Configured account alias(es), comma-separated")
	snapshotListCmd.Flags().StringVar(&snapshotListOU, "ou", "", "AWS OU ID(s), comma-separated (requires --payer)")
	snapshotListCmd.Flags().BoolVar(&snapshotListOUDirect, "ou-direct", false, "Include only accounts directly in --ou, not descendant OUs")
	snapshotListCmd.Flags().StringVar(&snapshotListPayer, "payer", "", "Registered payer alias for member IDs, --ou, or --tag-key")
	snapshotListCmd.Flags().StringVar(&snapshotListTagKey, "tag-key", "", "Select accounts by AWS Organizations tag key")
	snapshotListCmd.Flags().StringVar(&snapshotListTagValue, "tag-value", "", "Optional tag value for --tag-key")
	snapshotListCmd.Flags().IntVar(&snapshotListOlderThanDays, "older-than-days", snapshot.DefaultOlderThanDays, "List snapshots older than this many days")
	snapshotListCmd.Flags().StringVar(&snapshotListTypes, "types", "ebs,rds", "Snapshot types to scan: ebs, rds (comma-separated)")
	snapshotListCmd.Flags().StringVar(&snapshotListRegions, "regions", "", "Limit scan to comma-separated AWS regions (default: all enabled regions)")
	snapshotListCmd.Flags().Float64Var(&snapshotListMinSizeGiB, "min-size-gib", 0, "Skip snapshots smaller than this size in GiB")
	snapshotListCmd.Flags().StringVar(&snapshotListFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	snapshotListCmd.Flags().StringVar(&snapshotListRole, "role", "", "Linked-account IAM role name (default: config defaults.aws.linked_role)")
	snapshotListCmd.Flags().BoolVar(&snapshotListQuiet, "quiet", false, "Suppress progress messages on stderr")
	snapshotListCmd.Flags().BoolVar(&snapshotListSkipOrgCache, "skip-org-cache", false, "Bypass cached organization account/tag data")
	snapshotListCmd.Flags().BoolVar(&snapshotListRefreshOrgCache, "refresh-org-cache", false, "Refresh organization cache from AWS")
}

func runSnapshotList(cmd *cobra.Command, _ []string) error {
	format, err := output.ParseFormat(snapshotListFormat)
	if err != nil {
		return err
	}
	types, err := snapshot.ParseTypes(snapshotListTypes)
	if err != nil {
		return err
	}
	regions, err := snapshot.ParseRegions(snapshotListRegions)
	if err != nil {
		return err
	}

	cfgPath, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(cfgPath)
	if err != nil {
		return err
	}

	status := progress.New(cmd.ErrOrStderr(), snapshotListQuiet)

	sel, err := parseCostTargetSelector(
		snapshotListAccount, snapshotListAccountAliases, snapshotListOU, snapshotListPayer,
		snapshotListTagKey, snapshotListTagValue, snapshotListOUDirect,
		snapshotListSkipOrgCache, snapshotListRefreshOrgCache,
	)
	if err != nil {
		return err
	}

	targets, err := resolveCostTargets(
		cmd.Context(), cmd, cfg, sel,
		awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod,
		status,
	)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return output.WriteSnapshotListResult(cmd.OutOrStdout(), format, snapshot.Result{
			Summary: snapshot.Summary{
				OlderThanDays:  snapshotListOlderThanDays,
				CostDisclaimer: "Estimates use volume or allocated size; actual EBS snapshot billing may be lower.",
			},
		})
	}

	status.Step("Ensuring AWS credentials…")
	if err := ensureSnapshotCredentials(cmd.Context(), cmd, cfg, targets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
		return err
	}
	if len(targets) <= 1 {
		status.Step("Preparing account configuration…")
	}
	snapshotTargets, err := prepareSnapshotTargets(
		cmd.Context(), cmd, cfg, targets,
		awsFlags.CredentialsFile, awsFlags.ConfigPath, snapshotListRole,
		status,
	)
	if err != nil {
		return err
	}

	if len(snapshotTargets) > 1 {
		status.Step(fmt.Sprintf("Scanning %d account(s) for snapshots…", len(snapshotTargets)))
	}

	result, err := snapshotListFetch(cmd.Context(), snapshot.Query{
		Targets:    snapshotTargets,
		OlderThan:  time.Duration(snapshotListOlderThanDays) * 24 * time.Hour,
		Types:      types,
		Regions:    regions,
		MinSizeGiB: snapshotListMinSizeGiB,
		Progress:   status.Step,
	})
	if err != nil {
		return err
	}

	status.Step("Fetching billed snapshot costs from Cost Explorer…")
	billed, err := fetchSnapshotBilledCosts(cmd.Context(), cfg, targets, awsFlags.CredentialsFile, time.Now().UTC())
	if err != nil {
		status.Step(fmt.Sprintf("Warning: billed snapshot costs unavailable: %v", err))
	} else {
		result.Summary.BilledCosts = billed
	}

	return output.WriteSnapshotListResult(cmd.OutOrStdout(), format, result)
}
