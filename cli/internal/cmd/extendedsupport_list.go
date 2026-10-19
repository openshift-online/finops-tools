// extendedsupport_list.go implements "finops extended-support list".
package cmd

import (
	"fmt"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/spf13/cobra"
)

var (
	extendedSupportListAccount         string
	extendedSupportListAccountAliases  string
	extendedSupportListFormat          string
	extendedSupportListOutput          string
	extendedSupportListOU              string
	extendedSupportListPayer           string
	extendedSupportListQuiet           bool
	extendedSupportListRegions         string
	extendedSupportListRole            string
	extendedSupportListServices        string
	extendedSupportListSkipOrgCache    bool
	extendedSupportListRefreshOrgCache bool
	extendedSupportListTag             string
	extendedSupportListWorkers         int
	extendedSupportListFullScan        bool
	extendedSupportListBillingDays     int
	extendedSupportListFetch           = extendedsupport.Fetch
	extendedSupportListRetry           = extendedsupport.RetrySkippedAccounts
)

var extendedSupportListCmd = &cobra.Command{
	Use:   "list",
	Short: "List AWS resources running in extended support",
	Long: `Discover RDS DB instances, Aurora/RDS DB clusters, EKS Kubernetes clusters, and ElastiCache caches
running versions in AWS Extended Support.

Account selection matches finops account get-cost: --account-id, --account-alias, --ou, --tag, or --payer alone
(--ou/--payer select ACTIVE members only). Linked member accounts are scanned using role assumption from the payer.
With --payer alone, Cost Explorer narrows the scan to active members with recorded
Extended Support usage in the billing lookback window (default: 30 UTC days including today).
Billing data can be delayed. Use --full-scan to check every active member account.
Explicit account, OU, and tag selections scan all selected accounts.
Accounts that cannot be assumed into, or regions that fail credentialed API calls during the scan,
are skipped and listed under "Skipped accounts" or "Skipped regions" in the output.
After the initial scan, skipped accounts are retried once using fresh payer-session state.
The final skipped-account list contains only accounts that still could not be scanned.

RDS support dates come from DescribeDBMajorEngineVersions. EKS version catalogs are
preloaded by region and shared across accounts and the final retry pass. EKS clusters
are classified using their Kubernetes version and support dates, not their upgrade policy alone.
ElastiCache Redis OSS versions are checked against AWS's published support schedule,
bundled with the CLI because the ElastiCache API does not expose lifecycle dates.
Node-based caches and serverless caches are checked. Replication-group member
cache clusters are reported individually; their parent group is not counted again.
Unknown versions produce resource warnings. Valkey and Memcached have no announced
paid Extended Support schedule and are excluded.

Required IAM permissions in each scanned account:
  ec2:DescribeRegions
  rds:DescribeDBInstances, rds:DescribeDBClusters
  rds:DescribeDBEngineVersions, rds:DescribeDBMajorEngineVersions
  eks:ListClusters, eks:DescribeCluster, eks:DescribeClusterVersions
  elasticache:DescribeCacheClusters, elasticache:DescribeServerlessCaches
  elasticache:ListTagsForResource (missing tags produce warnings but retain resources)

Payer credentials also need sts:AssumeRole into the configured linked-account role.
Payer-wide billing selection needs ce:GetDimensionValues and ce:GetCostAndUsage.
Allow eks:DescribeClusterVersions on the payer to preload EKS catalogs; member
credentials provide a fallback if the payer cannot retrieve version information.

Examples:
  finops extended-support list --account-alias rh-control
  finops extended-support list --payer rh-control
  finops extended-support list --payer rh-control --billing-lookback-days 7
  finops extended-support list --payer rh-control --full-scan
  finops extended-support list --payer rh-control --services rds --format json
  finops extended-support list --payer rh-control --services eks --format json
  finops extended-support list --payer rh-control --services elasticache --format json
  finops extended-support list --payer rh-control --regions us-east-1,us-west-2`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		sel, err := parseCostTargetSelector(
			extendedSupportListAccount, extendedSupportListAccountAliases, extendedSupportListOU,
			extendedSupportListPayer, extendedSupportListTag,
			extendedSupportListSkipOrgCache, extendedSupportListRefreshOrgCache, false,
		)
		if err != nil {
			return err
		}
		if _, err := validateCostTargetSelector(sel); err != nil {
			return err
		}
		if _, err := output.ParseFormat(extendedSupportListFormat); err != nil {
			return err
		}
		if _, err := extendedsupport.ParseServices(extendedSupportListServices); err != nil {
			return err
		}
		if _, err := extendedsupport.ParseRegions(extendedSupportListRegions); err != nil {
			return err
		}
		if err := validateWorkers(extendedSupportListWorkers); err != nil {
			return err
		}
		if _, err := extendedsupport.BillingLookbackRange(time.Now(), extendedSupportListBillingDays); err != nil {
			return err
		}
		return validateOrgCacheFlags(extendedSupportListSkipOrgCache, extendedSupportListRefreshOrgCache)
	},
	RunE: runExtendedSupportList,
}

func init() {
	extendedSupportCmd.AddCommand(extendedSupportListCmd)
	bindAWSTargetFlags(extendedSupportListCmd, awsTargetFlagRefs{
		Account:         &extendedSupportListAccount,
		AccountAliases:  &extendedSupportListAccountAliases,
		OU:              &extendedSupportListOU,
		Payer:           &extendedSupportListPayer,
		Tag:             &extendedSupportListTag,
		SkipOrgCache:    &extendedSupportListSkipOrgCache,
		RefreshOrgCache: &extendedSupportListRefreshOrgCache,
	})
	extendedSupportListCmd.Flags().Lookup("payer").Usage = "Registered payer alias: alone selects members with billed Extended Support usage (--full-scan selects all active members); required with --ou/--tag or unregistered --account-id"
	extendedSupportListCmd.Flags().StringVar(&extendedSupportListServices, "services", "rds,eks,elasticache",
		"Services to scan: rds,eks,elasticache (comma-separated)")
	extendedSupportListCmd.Flags().StringVar(&extendedSupportListRegions, "regions", "",
		"Limit scan to comma-separated AWS regions (default: all enabled regions)")
	extendedSupportListCmd.Flags().BoolVar(&extendedSupportListFullScan, "full-scan", false,
		"With --payer alone, scan every active member instead of filtering accounts through Cost Explorer")
	extendedSupportListCmd.Flags().IntVar(&extendedSupportListBillingDays, "billing-lookback-days", 30,
		"UTC calendar days, including today, used for Cost Explorer account selection with --payer alone")
	extendedSupportListCmd.Flags().StringVar(&extendedSupportListFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	addOutputFlag(extendedSupportListCmd, &extendedSupportListOutput)
	bindLinkedRoleFlag(extendedSupportListCmd, &extendedSupportListRole)
	extendedSupportListCmd.Flags().BoolVar(&extendedSupportListQuiet, "quiet", false, "Suppress progress messages on stderr")
	bindWorkersFlag(extendedSupportListCmd, &extendedSupportListWorkers, "")
}

func runExtendedSupportList(cmd *cobra.Command, _ []string) error {
	format, err := output.ParseFormat(extendedSupportListFormat)
	if err != nil {
		return err
	}
	services, err := extendedsupport.ParseServices(extendedSupportListServices)
	if err != nil {
		return err
	}
	regions, err := extendedsupport.ParseRegions(extendedSupportListRegions)
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

	status := progress.New(cmd.ErrOrStderr(), extendedSupportListQuiet)

	sel, err := parseCostTargetSelector(
		extendedSupportListAccount, extendedSupportListAccountAliases, extendedSupportListOU,
		extendedSupportListPayer, extendedSupportListTag,
		extendedSupportListSkipOrgCache, extendedSupportListRefreshOrgCache, false,
	)
	if err != nil {
		return err
	}

	costTargets, err := resolveCostTargets(
		cmd, cfg, &sel,
		awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod,
		status,
	)
	if err != nil {
		return err
	}

	scanNow := time.Now().UTC()
	costTargets, billingSelection, err := filterExtendedSupportBillingTargets(
		cmd, cfg, sel, costTargets, extendedSupportListFullScan,
		extendedSupportListBillingDays, scanNow, status,
	)
	if err != nil {
		return err
	}

	out, closeOut, err := resolveCommandOutput(cmd, extendedSupportListOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}

	if len(costTargets) == 0 {
		return output.WriteExtendedSupportListResult(out, format, extendedsupport.Result{
			Summary: extendedsupport.Summary{BillingSelection: billingSelection},
		})
	}

	status.Step("Ensuring AWS credentials…")
	awsCtx := awsCommandContext(cmd)
	if err := ensureExtendedSupportCredentials(cmd, cfg, costTargets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
		return err
	}
	if len(costTargets) <= 1 {
		status.Step("Preparing account configuration…")
	}
	prepareBar := progress.NewBar(cmd.ErrOrStderr(), extendedSupportListQuiet, "Preparing account configuration…", len(costTargets))
	scanTargets, skippedAccounts, err := prepareExtendedSupportTargets(
		cmd, cfg, costTargets,
		awsFlags.CredentialsFile, awsFlags.ConfigPath, extendedSupportListRole,
		extendedSupportListWorkers,
		prepareBar,
	)
	if err != nil {
		return err
	}
	query := extendedsupport.Query{
		Targets:  scanTargets,
		Services: services,
		Regions:  regions,
		Workers:  extendedSupportListWorkers,
		Now:      scanNow,
	}
	for _, service := range services {
		if service == extendedsupport.ServiceEKS {
			query.EKSVersionCatalog = extendedsupport.NewEKSVersionCatalog()
			break
		}
	}
	var result extendedsupport.Result
	if len(scanTargets) > 0 {
		if len(scanTargets) == 1 {
			status.Step("Scanning account for extended-support resources…")
		}
		scanBar := progress.NewBar(cmd.ErrOrStderr(), extendedSupportListQuiet, "Scanning accounts for extended-support resources…", len(scanTargets))
		query.AccountProgress = scanBar
		result, err = extendedSupportListFetch(awsCtx, query)
		scanBar.Finish()
		if err != nil {
			return err
		}
	}
	result.Summary.SkippedAccounts = mergeExtendedSupportSkippedAccounts(skippedAccounts, result.Summary.SkippedAccounts)
	if len(result.Summary.SkippedAccounts) > 0 {
		if err := awsCtx.Err(); err != nil {
			return err
		}
		skippedIDs := make(map[string]bool, len(result.Summary.SkippedAccounts))
		for _, warning := range result.Summary.SkippedAccounts {
			skippedIDs[warning.AccountID] = true
		}
		var retryTargets []cost.AccountTarget
		for _, target := range costTargets {
			if skippedIDs[target.AccountID] {
				retryTargets = append(retryTargets, target)
			}
		}
		if len(retryTargets) > 0 {
			status.Step(fmt.Sprintf("Retrying %d skipped account(s) once…", len(retryTargets)))
			// A new preparation gives retries fresh payer-session state, including
			// another chance to renew a login that failed during the first pass.
			retryScanTargets, retrySkipped, err := prepareExtendedSupportTargets(
				cmd, cfg, retryTargets,
				awsFlags.CredentialsFile, awsFlags.ConfigPath, extendedSupportListRole,
				extendedSupportListWorkers, nil,
			)
			if err != nil {
				return err
			}
			result.Summary.SkippedAccounts = mergeExtendedSupportSkippedAccounts(result.Summary.SkippedAccounts, retrySkipped)
			retryBar := progress.NewBar(cmd.ErrOrStderr(), extendedSupportListQuiet, "Retrying skipped accounts…", len(retryScanTargets))
			query.Targets = retryScanTargets
			query.AccountProgress = retryBar
			result, err = extendedSupportListRetry(awsCtx, query, result)
			retryBar.Finish()
			if err != nil {
				return err
			}
		}
	}

	result.Summary.BillingSelection = billingSelection
	return output.WriteExtendedSupportListResult(out, format, result)
}
