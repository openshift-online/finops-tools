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
	accountGetCostAccount         string
	accountGetCostAccountAliases  string
	accountGetCostFormat          string
	accountGetCostOutput          string
	accountGetCostOU              string
	accountGetCostOUDirect        bool
	accountGetCostPayer           string
	accountGetCostProvider        string
	accountGetCostSplitBy         string
	accountGetCostTagKey          string
	accountGetCostTagValue        string
	accountGetCostQuiet           bool
	accountGetCostSkipOrgCache    bool
	accountGetCostRefreshOrgCache bool
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
		sel, err := parseAccountTargetSelector(
			accountGetCostAccount, accountGetCostAccountAliases, accountGetCostOU, accountGetCostPayer,
			accountGetCostTagKey, accountGetCostTagValue, accountGetCostOUDirect,
			accountGetCostSkipOrgCache, accountGetCostRefreshOrgCache,
		)
		if err != nil {
			return err
		}
		if _, err := validateAccountTargetSelector(sel); err != nil {
			return err
		}
		if err := validatePeriodFlags(cmd); err != nil {
			return err
		}
		if _, err := output.ParseFormat(accountGetCostFormat); err != nil {
			return err
		}
		if _, err := cost.ParseProvider(accountGetCostProvider); err != nil {
			return err
		}
		if _, err := cost.ParseSplitBy(accountGetCostSplitBy); err != nil {
			return err
		}
		return validateOrgCacheFlags(accountGetCostSkipOrgCache, accountGetCostRefreshOrgCache)
	},
	RunE: runAccountGetCost,
}

func init() {
	accountCmd.AddCommand(accountGetCostCmd)
	bindAWSTargetFlags(accountGetCostCmd, awsTargetFlagRefs{
		Account:         &accountGetCostAccount,
		AccountAliases:  &accountGetCostAccountAliases,
		OU:              &accountGetCostOU,
		OUDirect:        &accountGetCostOUDirect,
		Payer:           &accountGetCostPayer,
		TagKey:          &accountGetCostTagKey,
		TagValue:        &accountGetCostTagValue,
		SkipOrgCache:    &accountGetCostSkipOrgCache,
		RefreshOrgCache: &accountGetCostRefreshOrgCache,
	})
	accountGetCostCmd.Flags().StringVar(&accountGetCostFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	addOutputFlag(accountGetCostCmd, &accountGetCostOutput)
	accountGetCostCmd.Flags().StringVar(&accountGetCostProvider, "provider", string(cost.ProviderAWS),
		"Cloud provider: aws or gcp")
	accountGetCostCmd.Flags().StringVar(&accountGetCostSplitBy, "split-by", "",
		"Split results by dimension (supported: service, account)")
	accountGetCostCmd.Flags().BoolVar(&accountGetCostQuiet, "quiet", false, "Suppress progress messages on stderr")
	addPeriodFlags(accountGetCostCmd)
}

func runAccountGetCost(cmd *cobra.Command, _ []string) error {
	format, err := output.ParseFormat(accountGetCostFormat)
	if err != nil {
		return err
	}
	provider, err := cost.ParseProvider(accountGetCostProvider)
	if err != nil {
		return err
	}
	splitBy, err := cost.ParseSplitBy(accountGetCostSplitBy)
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

	status := progress.New(cmd.ErrOrStderr(), accountGetCostQuiet)
	awsCtx := cmd.Context()
	if provider == cost.ProviderAWS {
		awsCtx = awsCommandContext(cmd)
	}

	sel, err := parseAccountTargetSelector(
		accountGetCostAccount, accountGetCostAccountAliases, accountGetCostOU, accountGetCostPayer,
		accountGetCostTagKey, accountGetCostTagValue, accountGetCostOUDirect,
		accountGetCostSkipOrgCache, accountGetCostRefreshOrgCache,
	)
	if err != nil {
		return err
	}

	targets, err := resolveAccountTargets(
		cmd, cfg, sel,
		awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod,
		status,
	)
	if err != nil {
		return err
	}

	out, closeOut, err := resolveCommandOutput(cmd, accountGetCostOutput)
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
		if err := ensureAccountCredentials(awsCtx, cmd, cfg, targets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
			return err
		}
		if len(targets) <= 1 {
			status.Step("Preparing account configuration…")
		}
		targets, err = prepareAccountTargets(awsCtx, cfg, targets, awsFlags.CredentialsFile, status)
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
