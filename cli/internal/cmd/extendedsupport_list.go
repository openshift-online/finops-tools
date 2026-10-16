// extendedsupport_list.go implements "finops extended-support list".
package cmd

import (
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
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
	extendedSupportListFetch           = extendedsupport.Fetch
)

var extendedSupportListCmd = &cobra.Command{
	Use:   "list",
	Short: "List AWS resources running in extended support",
	Long: `Discover RDS DB instances and Aurora/RDS DB clusters whose engine version is past
its standard support end date (AWS Extended Support).

Account selection matches finops account get-cost: --account-id, --account-alias, --ou, --tag, or --payer alone
(--ou/--payer select ACTIVE members only). Linked member accounts are scanned using role assumption from the payer.
Accounts that cannot be assumed into, or regions that fail credentialed API calls during the scan,
are skipped and listed under "Skipped accounts" or "Skipped regions" in the output.

A resource is reported when its engine major version's standard support end date (from
RDS DescribeDBMajorEngineVersions) is in the past.

Required IAM permissions in each scanned account:
  ec2:DescribeRegions
  rds:DescribeDBInstances, rds:DescribeDBClusters
  rds:DescribeDBEngineVersions, rds:DescribeDBMajorEngineVersions

Payer credentials also need sts:AssumeRole into the configured linked-account role.

Examples:
  finops extended-support list --account-alias rh-control
  finops extended-support list --payer rh-control
  finops extended-support list --payer rh-control --services rds --format json
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
	extendedSupportListCmd.Flags().StringVar(&extendedSupportListServices, "services", "rds",
		"Services to scan: rds (comma-separated)")
	extendedSupportListCmd.Flags().StringVar(&extendedSupportListRegions, "regions", "",
		"Limit scan to comma-separated AWS regions (default: all enabled regions)")
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

	out, closeOut, err := resolveCommandOutput(cmd, extendedSupportListOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}

	if len(costTargets) == 0 {
		return output.WriteExtendedSupportListResult(out, format, extendedsupport.Result{})
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
	if len(scanTargets) == 0 {
		return output.WriteExtendedSupportListResult(out, format, extendedsupport.Result{
			Summary: extendedsupport.Summary{
				SkippedAccounts: skippedAccounts,
			},
		})
	}

	if len(scanTargets) <= 1 {
		status.Step("Scanning account for extended-support resources…")
	}
	scanBar := progress.NewBar(cmd.ErrOrStderr(), extendedSupportListQuiet, "Scanning accounts for extended-support resources…", len(scanTargets))
	result, err := extendedSupportListFetch(awsCtx, extendedsupport.Query{
		Targets:         scanTargets,
		Services:        services,
		Regions:         regions,
		AccountProgress: scanBar,
		Workers:         extendedSupportListWorkers,
	})
	if scanBar != nil {
		scanBar.Finish()
	}
	if err != nil {
		return err
	}
	result.Summary.SkippedAccounts = mergeExtendedSupportSkippedAccounts(skippedAccounts, result.Summary.SkippedAccounts)

	return output.WriteExtendedSupportListResult(out, format, result)
}
