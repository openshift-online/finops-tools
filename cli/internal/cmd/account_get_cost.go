// account_get_cost.go implements "finops account get-cost": resolves targets, ensures credentials, fetches costs, and prints output.
package cmd

import (
	"fmt"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

var (
	costGetAccount         string
	costGetAccountAliases  string
	costGetFormat          string
	costGetOutput          string
	costGetOU              string
	costGetOUDirect        bool
	costGetPayer           string
	costGetProvider        string
	costGetSplitBy         string
	costGetTagKey          string
	costGetTagValue        string
	costGetQuiet           bool
	costGetSkipOrgCache    bool
	costGetRefreshOrgCache bool
)

var accountGetCostCmd = &cobra.Command{
	Use:   "get-cost",
	Short: "Get net amortized cost for a date range",
	Long: `Fetch the sum of AWS Cost Explorer NetAmortizedCost for one or more payer or linked accounts.
Provide --account with 12-digit AWS account IDs and/or --account-alias with configured aliases (see finops config account add aws).
Alternatively, select accounts by AWS Organizations tag with --payer and --tag-key (optional --tag-value).

Period (default: last 30 calendar days, or defaults.cost.* in config):
  --days, --months, --from/--to, --exclude-recent-days (omit recent incomplete CE days)

For linked accounts, credentials are obtained from the registered payer account.
Use --payer with --account to query a member account that is not registered (the payer alias must be registered).
Use --payer with --tag-key to query all org accounts matching an Organizations account tag.

Examples:
  finops account get-cost --account-alias rh-control
  finops account get-cost --payer rh-control --tag-key organization
  finops account get-cost --payer rh-control --tag-key organization --tag-value "Hybrid Platform" --split-by service

Use --ou with --payer to query all accounts in an AWS Organizational Unit (recursive by default).
Add --ou-direct to include only accounts directly in the OU, not descendant OUs.

Examples:
  finops account get-cost --ou ou-abcd-1234 --payer rh-control
  finops account get-cost --ou ou-abcd-1234 --payer rh-control --ou-direct --days 7

Authentication uses --auth-method when set, otherwise defaults.aws.auth_method in config (saml by default).

Only AWS is supported today; GCP will be added later.`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		sel, err := parseCostTargetSelector(
			costGetAccount, costGetAccountAliases, costGetOU, costGetPayer,
			costGetTagKey, costGetTagValue, costGetOUDirect,
			costGetSkipOrgCache, costGetRefreshOrgCache,
		)
		if err != nil {
			return err
		}
		if _, err := validateCostTargetSelector(sel); err != nil {
			return err
		}
		if err := validatePeriodFlags(cmd); err != nil {
			return err
		}
		if _, err := output.ParseFormat(costGetFormat); err != nil {
			return err
		}
		if _, err := cost.ParseProvider(costGetProvider); err != nil {
			return err
		}
		if _, err := cost.ParseSplitBy(costGetSplitBy); err != nil {
			return err
		}
		return validateOrgCacheFlags(costGetSkipOrgCache, costGetRefreshOrgCache)
	},
	RunE: runAccountGetCost,
}

func init() {
	accountCmd.AddCommand(accountGetCostCmd)
	bindAWSTargetFlags(accountGetCostCmd, awsTargetFlagRefs{
		Account:         &costGetAccount,
		AccountAliases:  &costGetAccountAliases,
		OU:              &costGetOU,
		OUDirect:        &costGetOUDirect,
		Payer:           &costGetPayer,
		TagKey:          &costGetTagKey,
		TagValue:        &costGetTagValue,
		SkipOrgCache:    &costGetSkipOrgCache,
		RefreshOrgCache: &costGetRefreshOrgCache,
	})
	accountGetCostCmd.Flags().StringVar(&costGetFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	addOutputFlag(accountGetCostCmd, &costGetOutput)
	accountGetCostCmd.Flags().StringVar(&costGetProvider, "provider", string(cost.ProviderAWS),
		"Cloud provider: aws or gcp")
	accountGetCostCmd.Flags().StringVar(&costGetSplitBy, "split-by", "",
		"Split results by dimension (supported: service, account)")
	accountGetCostCmd.Flags().BoolVar(&costGetQuiet, "quiet", false, "Suppress progress messages on stderr")
	addPeriodFlags(accountGetCostCmd)
}

func runAccountGetCost(cmd *cobra.Command, _ []string) error {
	format, err := output.ParseFormat(costGetFormat)
	if err != nil {
		return err
	}
	provider, err := cost.ParseProvider(costGetProvider)
	if err != nil {
		return err
	}
	splitBy, err := cost.ParseSplitBy(costGetSplitBy)
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
	if err := applyCostPeriodDefaults(cmd, cfg); err != nil {
		return err
	}

	status := progress.New(cmd.ErrOrStderr(), costGetQuiet)
	awsCtx := cmd.Context()
	if provider == cost.ProviderAWS {
		awsCtx = awsCommandContext(cmd)
	}

	sel, err := parseCostTargetSelector(
		costGetAccount, costGetAccountAliases, costGetOU, costGetPayer,
		costGetTagKey, costGetTagValue, costGetOUDirect,
		costGetSkipOrgCache, costGetRefreshOrgCache,
	)
	if err != nil {
		return err
	}

	targets, err := resolveCostTargets(
		cmd, cfg, sel,
		awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod,
		status,
	)
	if err != nil {
		return err
	}

	out, closeOut, err := resolveCommandOutput(cmd, costGetOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}

	if len(targets) == 0 {
		dateRange, err := resolveCostPeriod(time.Now().UTC())
		if err != nil {
			return err
		}
		return output.WriteCostResult(out, format, cost.EmptyResult(provider, dateRange, splitBy))
	}

	if provider == cost.ProviderAWS {
		status.Step("Ensuring AWS credentials…")
		if err := ensureCostCredentials(awsCtx, cmd, cfg, targets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
			return err
		}
		if len(targets) <= 1 {
			status.Step("Preparing account configuration…")
		}
		targets, err = prepareCostTargets(awsCtx, cfg, targets, awsFlags.CredentialsFile, status)
		if err != nil {
			return err
		}
	}

	dateRange, err := resolveCostPeriod(time.Now().UTC())
	if err != nil {
		return err
	}

	if len(targets) > 1 {
		status.Step(fmt.Sprintf("Fetching net amortized costs for %d account(s) from AWS Cost Explorer…", len(targets)))
	}

	costQuery := cost.CostQuery{
		Provider: provider,
		Accounts: targets,
		Range:    dateRange,
		SplitBy:  splitBy,
		Progress: status,
	}
	if provider == cost.ProviderAWS && splitBy == cost.SplitByAccount {
		costQuery.AWSFetch = &cost.AWSFetchOptions{
			ResolveAccountNames: coreaccount.ResolveAccountNames,
		}
	}

	result, err := cost.Fetch(awsCtx, costQuery)
	if err != nil {
		return err
	}

	return output.WriteCostResult(out, format, result)
}
