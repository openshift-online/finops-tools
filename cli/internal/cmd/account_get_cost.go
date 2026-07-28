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
	costGetPayer           string
	costGetProvider        string
	costGetSplitBy         string
	costGetTag             string
	costGetQuiet           bool
	costGetSkipOrgCache    bool
	costGetRefreshOrgCache bool
	costGetWorkers         int
)

var accountGetCostCmd = &cobra.Command{
	Use:   "get-cost",
	Short: "Get net amortized cost for a date range",
	Long: `Fetch the sum of AWS Cost Explorer NetAmortizedCost for one or more payer or linked accounts.

Account selection (exactly one mode):
  --account-id / --account-alias   Explicit accounts (optional --payer for unregistered member IDs)
  --payer                       All active member accounts in the payer's organization
  --payer --ou                  Accounts under an OU or org root (scope suffix on each ID)
  --payer --tag KEY[=VALUE]     Accounts matching an Organizations tag

--ou scope suffixes (per ID):
  ou-xxxx / r-xxxx      all accounts under parent (default)
  ou-xxxx/              accounts directly in that OU/root only
  ou-xxxx/*             that OU/root + immediate child OUs only
  ou-xxxx/**            same as bare ID (explicit subtree)

Period (default: last 30 calendar days, or defaults.cost.* in config):
  --days, --months, --from/--to, --exclude-recent-days (omit recent incomplete CE days)

Examples:
  finops account get-cost --account-alias rh-control
  finops account get-cost --payer rh-control
  finops account get-cost --payer rh-control --account-id 111111111111,222222222222
  finops account get-cost --payer rh-control --tag organization
  finops account get-cost --payer rh-control --tag organization="Hybrid Platform" --split-by service
  finops account get-cost --ou ou-abcd-12345678 --payer rh-control
  finops account get-cost --ou ou-abcd-12345678/ --payer rh-control --days 7
  finops account get-cost --ou 'ou-abcd-12345678/*' --payer rh-control
  finops account get-cost --payer rh-control --split-by ou

Authentication uses --auth-method when set, otherwise defaults.aws.auth_method in config (saml by default).

Only AWS is supported today; GCP will be added later.`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		sel, err := parseCostTargetSelector(
			costGetAccount, costGetAccountAliases, costGetOU, costGetPayer,
			costGetTag,
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
		if err := validateWorkers(costGetWorkers); err != nil {
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
		Payer:           &costGetPayer,
		Tag:             &costGetTag,
		SkipOrgCache:    &costGetSkipOrgCache,
		RefreshOrgCache: &costGetRefreshOrgCache,
	})
	accountGetCostCmd.Flags().StringVar(&costGetFormat, "format", string(output.FormatPrettyPrint),
		"Output format: pretty-print, json, csv")
	addOutputFlag(accountGetCostCmd, &costGetOutput)
	accountGetCostCmd.Flags().StringVar(&costGetProvider, "provider", string(cost.ProviderAWS),
		"Cloud provider: aws or gcp")
	accountGetCostCmd.Flags().StringVar(&costGetSplitBy, "split-by", "",
		"Split results by dimension (supported: service, account, ou)")
	accountGetCostCmd.Flags().BoolVar(&costGetQuiet, "quiet", false, "Suppress progress messages on stderr")
	bindWorkersFlag(accountGetCostCmd, &costGetWorkers, "")
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
		costGetTag,
		costGetSkipOrgCache, costGetRefreshOrgCache,
	)
	if err != nil {
		return err
	}

	targets, err := resolveCostTargets(
		cmd, cfg, &sel,
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
		prepareBar := progress.NewBar(cmd.ErrOrStderr(), costGetQuiet, "Preparing account configuration…", len(targets))
		targets, err = prepareCostTargets(awsCtx, cfg, targets, awsFlags.CredentialsFile, prepareBar)
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
		Workers:  costGetWorkers,
	}
	if provider == cost.ProviderAWS && (splitBy == cost.SplitByAccount || splitBy == cost.SplitByOU) {
		awsFetch := &cost.AWSFetchOptions{}
		if splitBy == cost.SplitByAccount {
			awsFetch.ResolveAccountNames = coreaccount.ResolveAccountNames
		}
		if splitBy == cost.SplitByOU {
			status.Step("Mapping accounts to organizational units…")
			buckets, hierarchy, _, err := resolveAccountOUBuckets(awsCtx, cfg, sel, targets, awsFlags.CredentialsFile)
			if err != nil {
				return err
			}
			awsFetch.AccountOUBuckets = buckets
			awsFetch.OUHierarchy = hierarchy
		}
		costQuery.AWSFetch = awsFetch
	}

	result, err := cost.Fetch(awsCtx, costQuery)
	if err != nil {
		return err
	}

	return output.WriteCostResult(out, format, result)
}
