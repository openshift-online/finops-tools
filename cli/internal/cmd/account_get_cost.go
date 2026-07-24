// account_get_cost.go implements "finops account get-cost": resolves targets, ensures credentials, fetches costs, and prints output.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
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
	costGetParentOU        string
	costGetPayer           string
	costGetProvider        string
	costGetSplitBy         string
	costGetTagKey          string
	costGetTagValue        string
	costGetQuiet           bool
	costGetSkipOrgCache    bool
	costGetRefreshOrgCache bool
)

// rootIDPattern matches AWS Organizations root IDs (e.g. r-xxxx).
var rootIDPattern = regexp.MustCompile(`^r-[0-9a-z]{4,32}$`)

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

Use --parent-ou with --payer to query all direct child OUs of a parent OU, reporting each individually.
Add --ou-direct to include only accounts directly in each child OU, not their sub-OUs.

Examples:
  finops account get-cost --parent-ou ou-abcd-1234 --payer rh-control
  finops account get-cost --parent-ou ou-abcd-1234 --payer rh-control --ou-direct --days 7

Authentication uses --auth-method when set, otherwise defaults.aws.auth_method in config (saml by default).

Only AWS is supported today; GCP will be added later.`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if costGetParentOU != "" {
			if err := validateParentOUFlags(); err != nil {
				return err
			}
		} else {
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
			if err := validateOrgCacheFlags(costGetSkipOrgCache, costGetRefreshOrgCache); err != nil {
				return err
			}
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
		return nil
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
	accountGetCostCmd.Flags().StringVar(&costGetParentOU, "parent-ou", "",
		"AWS parent OU ID; reports cost for each direct child OU individually (requires --payer)")
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

	out, closeOut, err := resolveCommandOutput(cmd, costGetOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}

	if costGetParentOU != "" {
		return runAccountGetCostByParentOU(cmd, cfg, format, provider, splitBy, status, out)
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

func validateParentOUFlags() error {
	if costGetPayer == "" {
		return fmt.Errorf("--parent-ou requires --payer")
	}
	if costGetOU != "" {
		return fmt.Errorf("--parent-ou and --ou are mutually exclusive")
	}
	if costGetTagKey != "" {
		return fmt.Errorf("--parent-ou and --tag-key are mutually exclusive")
	}
	if costGetAccount != "" || costGetAccountAliases != "" {
		return fmt.Errorf("--parent-ou and --account/--account-alias are mutually exclusive")
	}
	id := strings.TrimSpace(costGetParentOU)
	if !rootIDPattern.MatchString(id) {
		ids, err := configstore.ParseOUIDs(id)
		if err != nil {
			return fmt.Errorf("--parent-ou: %w", err)
		}
		if len(ids) != 1 {
			return fmt.Errorf("--parent-ou accepts a single OU or root ID, got %d", len(ids))
		}
	}
	return nil
}

func runAccountGetCostByParentOU(
	cmd *cobra.Command,
	cfg configstore.File,
	format output.Format,
	provider cost.Provider,
	splitBy cost.SplitBy,
	status *progress.Writer,
	out io.Writer,
) error {
	ctx := awsCommandContext(cmd)

	payerID, ok := cfg.PayerAccountIDForAlias(costGetPayer)
	if !ok {
		return fmt.Errorf("unknown payer alias %q (register payer with: finops config account add aws <12-digit-id> --alias %s)", costGetPayer, costGetPayer)
	}

	status.Step("Ensuring AWS credentials for payer…")
	payerTarget := cost.AccountTarget{AccountID: payerID}
	if err := ensureCostCredentials(ctx, cmd, cfg, []cost.AccountTarget{payerTarget}, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
		return err
	}

	payerCfg, err := loadAWSConfigForCredentialsAccount(ctx, cfg, payerID, awsFlags.CredentialsFile)
	if err != nil {
		return err
	}

	status.Step(fmt.Sprintf("Listing child OUs under %s…", costGetParentOU))
	childOUs, err := coreaccount.ListOrganizationalUnits(ctx, payerCfg, costGetParentOU)
	if err != nil {
		return fmt.Errorf("list child OUs for %s: %w", costGetParentOU, err)
	}
	showHeaders := len(childOUs) > 0
	if !showHeaders {
		if rootIDPattern.MatchString(costGetParentOU) {
			return fmt.Errorf("no child OUs found under root %s", costGetParentOU)
		}
		status.Step(fmt.Sprintf("No child OUs found under %s, querying OU directly…", costGetParentOU))
		childOUs = []coreaccount.OrganizationalUnit{{ID: costGetParentOU}}
	} else {
		status.Step(fmt.Sprintf("Found %d child OUs, fetching costs…", len(childOUs)))
	}

	dateRange, err := resolveCostPeriod(time.Now().UTC())
	if err != nil {
		return err
	}

	var jsonResults []cost.CostResult

	if format == output.FormatCSV {
		var ouCols []string
		if showHeaders {
			ouCols = []string{"ou_id", "ou_name"}
		}
		if err := output.WriteCostCSVHeader(out, splitBy, ouCols...); err != nil {
			return err
		}
	}

	for i, ou := range childOUs {
		ouLabel := ou.ID
		if ou.Name != "" {
			ouLabel = fmt.Sprintf("%s (%s)", ou.Name, ou.ID)
		}

		status.Step(fmt.Sprintf("[%d/%d] Resolving accounts in OU %s…", i+1, len(childOUs), ouLabel))

		accounts, err := coreaccount.ListAccountsInOU(ctx, payerCfg, ou.ID, coreaccount.ListAccountsInOUOptions{
			DirectOnly: costGetOUDirect,
		})
		if err != nil {
			return fmt.Errorf("OU %s: list accounts: %w", ou.ID, err)
		}
		if len(accounts) == 0 {
			status.Step(fmt.Sprintf("[%d/%d] No active accounts in OU %s, skipping", i+1, len(childOUs), ouLabel))
			continue
		}

		memberIDs := make([]string, 0, len(accounts))
		for _, acct := range accounts {
			memberIDs = append(memberIDs, acct.ID)
		}

		targets, err := configstore.ResolveOUAccountTargets(cfg, memberIDs, costGetPayer)
		if err != nil {
			return fmt.Errorf("OU %s: resolve targets: %w", ou.ID, err)
		}

		if err := ensureCostCredentials(ctx, cmd, cfg, targets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
			return fmt.Errorf("OU %s: credentials: %w", ou.ID, err)
		}

		targets, err = prepareCostTargets(ctx, cfg, targets, awsFlags.CredentialsFile, status)
		if err != nil {
			return fmt.Errorf("OU %s: prepare targets: %w", ou.ID, err)
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

		result, err := cost.Fetch(ctx, costQuery)
		if err != nil {
			return fmt.Errorf("OU %s: fetch cost: %w", ou.ID, err)
		}

		switch format {
		case output.FormatJSON:
			jsonResults = append(jsonResults, result)
		case output.FormatCSV:
			var ouVals []string
			if showHeaders {
				ouVals = []string{ou.ID, ou.Name}
			}
			if err := output.WriteCostResultCSV(out, result, ouVals...); err != nil {
				return err
			}
		default:
			if showHeaders {
				if i > 0 {
					if _, err := fmt.Fprintln(out); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintf(out, "── %s ──\n", ouLabel); err != nil {
					return err
				}
			}
			if err := output.WriteCostResult(out, format, result); err != nil {
				return err
			}
		}
	}

	if format == output.FormatJSON {
		if !showHeaders && len(jsonResults) == 1 {
			return output.WriteCostResult(out, format, jsonResults[0])
		}
		data, err := json.MarshalIndent(jsonResults, "", "  ")
		if err != nil {
			return fmt.Errorf("encode results: %w", err)
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	}

	return nil
}
