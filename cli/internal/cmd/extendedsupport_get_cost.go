package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/spf13/cobra"
)

var (
	fetchExtendedSupportCosts            = extendedsupport.FetchCosts
	resolveExtendedSupportCostTargets    = resolveExtendedSupportCostTargetsImpl
	ensureExtendedSupportCostCredentials = ensureCostCredentialsImpl
	extendedSupportCostNow               = time.Now
)

type extendedSupportCostFlags struct {
	account, aliases, payer, ou, tag                       string
	services, groupBy, granularity, metric, format, output string
	period                                                 periodFlagValues
	quiet, skipOrgCache, refreshOrgCache                   bool
	regions                                                string
	skipRegions                                            []string
}

func init() {
	extendedSupportCmd.AddCommand(newExtendedSupportGetCostCommand())
}

func newExtendedSupportGetCostCommand() *cobra.Command {
	f := &extendedSupportCostFlags{}
	cmd := &cobra.Command{
		Use:   "get-cost",
		Short: "Get historical Extended Support charges by service or account",
		Long: `Query Cost Explorer for recorded Extended Support usage without scanning resources
or assuming member roles. Services: rds, eks, elasticache, opensearch (all by default).

The default period is equivalent to --days 90 --exclude-recent-days 2, using
the same period handling as account get-cost and AWS reports.
--days, --months, or --from/--to select the period. Dates are inclusive UTC dates.
The exclusion also applies to explicit periods: --from alone ends at the buffered
cutoff, and --to cannot exceed that cutoff. Use --exclude-recent-days 0 to include
recent complete days through yesterday.
With no explicit period or granularity, show one total per service across the
last 90 reliable Cost Explorer days. Explicit periods default to monthly buckets;
use --granularity daily or monthly to request a time breakdown.
--group-by defaults to service; account and service,account are also supported.

--regions limits costs to the listed AWS billing regions (all by default).
--skip-regions excludes regions and takes precedence over --regions. These
filters apply to billing data without discovering currently enabled regions.

--payer alone queries each payer's entire bill, including the payer and historical
charges for accounts that are now closed. Explicit account selections are scoped
to those accounts. OU/tag selections use current Organizations membership/tags.

Amounts default to NetUnblendedCost, including applicable discounts. Use --metric
UnblendedCost explicitly when gross charges are wanted. Missing metrics fail the
report rather than silently falling back to another cost basis.

Avoidable amounts identify historical, separately billed support surcharges;
they are not forecasts or total resource costs. EKS billing may include its base
control-plane fee, so EKS avoidable amounts are unavailable without a verified
baseline. Known avoidable subtotals exclude EKS; unknown amounts are never zero.

Payer billing credentials need ce:GetDimensionValues and ce:GetCostAndUsage.
OU/tag selection also needs the corresponding Organizations permissions.
Output formats: pretty-print (default), json, csv.`,
		Example: `  finops extended-support get-cost --payer rh-control
  finops extended-support get-cost --payer rh-control --services rds,eks
  finops extended-support get-cost --payer rh-control --regions us-east-1,us-west-2 --skip-regions us-west-2
  finops extended-support get-cost --payer rh-control --from 2026-08-01 --to 2026-10-31 --group-by service --granularity monthly
  finops extended-support get-cost --payer rh-control --group-by service,account --format json
  finops extended-support get-cost --account-alias my-account --days 7 --format csv -o support-costs.csv`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			_, _, err := f.query(cmd, extendedSupportCostNow())
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return f.run(cmd) },
	}
	bindAWSTargetFlags(cmd, awsTargetFlagRefs{
		Account: &f.account, AccountAliases: &f.aliases, Payer: &f.payer, OU: &f.ou, Tag: &f.tag,
		SkipOrgCache: &f.skipOrgCache, RefreshOrgCache: &f.refreshOrgCache,
	})
	cmd.Flags().Lookup("payer").Usage = "Registered payer alias(es), comma-separated. Alone queries the whole bill including historical/closed accounts; required with --ou/--tag; supplies billing credentials for explicit member accounts"
	cmd.Flags().StringVar(&f.services, "services", "rds,eks,elasticache,opensearch", "Services: rds,eks,elasticache,opensearch (comma-separated)")
	cmd.Flags().StringVar(&f.regions, "regions", "", "Limit costs to comma-separated AWS billing regions (default: all billing regions)")
	bindSkipRegionsFlag(cmd, &f.skipRegions)
	cmd.Flags().Lookup("skip-regions").Usage = "Exclude AWS billing regions (comma-separated or repeatable; overrides --regions)"
	cmd.Flags().StringVar(&f.groupBy, "group-by", "service", "Group by service, account, or service,account")
	bindPeriodFlags(cmd, &f.period, periodFlagValues{days: 90, excludeRecentDays: 2})
	cmd.Flags().StringVar(&f.granularity, "granularity", "", "daily or monthly (default: totals for the default period; monthly for an explicit period)")
	cmd.Flags().StringVar(&f.metric, "metric", "NetUnblendedCost", "Cost metric: NetUnblendedCost or UnblendedCost")
	cmd.Flags().StringVar(&f.format, "format", string(output.FormatPrettyPrint), "Output format: pretty-print, json, csv")
	cmd.Flags().BoolVar(&f.quiet, "quiet", false, "Suppress progress messages on stderr")
	addOutputFlag(cmd, &f.output)
	return cmd
}

func (f *extendedSupportCostFlags) query(cmd *cobra.Command, now time.Time) (extendedsupport.CostQuery, costTargetSelector, error) {
	q := extendedsupport.CostQuery{}
	sel, err := parseCostTargetSelector(f.account, f.aliases, f.ou, f.payer, f.tag, f.skipOrgCache, f.refreshOrgCache, true)
	if err != nil {
		return q, sel, err
	}
	if _, err := validateCostTargetSelector(sel); err != nil {
		return q, sel, err
	}
	if err := validateOrgCacheFlags(f.skipOrgCache, f.refreshOrgCache); err != nil {
		return q, sel, err
	}
	if _, err := output.ParseFormat(f.format); err != nil {
		return q, sel, err
	}
	if q.Services, err = extendedsupport.ParseServices(f.services); err != nil {
		return q, sel, err
	}
	if q.Regions, err = extendedsupport.ParseRegions(f.regions); err != nil {
		return q, sel, err
	}
	q.Regions = awsregions.Normalize(q.Regions)
	if q.SkipRegions, err = skipRegionsForCommand(cmd, f.skipRegions); err != nil {
		return q, sel, err
	}
	if q.GroupBy, err = extendedsupport.ParseCostGroupBy(f.groupBy); err != nil {
		return q, sel, err
	}
	if q.Metric, err = extendedsupport.ParseCostMetric(f.metric); err != nil {
		return q, sel, err
	}
	period := f.period
	// The default rolling window is a fallback, not a second period mode.
	if !cmd.Flags().Changed("days") && (period.months > 0 || strings.TrimSpace(period.from) != "" || strings.TrimSpace(period.to) != "") {
		period.days = 0
	}
	if err := validatePeriodFlagValues(cmd, period); err != nil {
		return q, sel, err
	}
	if q.Range, err = cost.ResolvePeriod(period.spec(), now); err != nil {
		return q, sel, err
	}
	q.Granularity, err = extendedsupport.ParseCostGranularity(f.granularity)
	q.TotalsOnly = !cmd.Flags().Changed("granularity") &&
		!cmd.Flags().Changed("days") && !cmd.Flags().Changed("months") &&
		!cmd.Flags().Changed("from") && !cmd.Flags().Changed("to")
	return q, sel, err
}

func (f *extendedSupportCostFlags) run(cmd *cobra.Command) error {
	query, sel, err := f.query(cmd, extendedSupportCostNow())
	if err != nil {
		return err
	}
	path, err := configstore.ResolvePath(awsFlags.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		return err
	}
	status := progress.New(cmd.ErrOrStderr(), f.quiet)
	targets, err := resolveExtendedSupportCostTargets(cmd, cfg, &sel, status)
	if err != nil {
		return err
	}
	ctx := awsCommandContext(cmd)
	hasBillingRegions := len(query.Regions) == 0 || len(awsregions.Exclude(query.Regions, query.SkipRegions)) > 0
	if len(targets) > 0 && hasBillingRegions {
		status.Step("Ensuring billing credentials…")
		if err := ensureExtendedSupportCostCredentials(ctx, cmd, cfg, targets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
			return err
		}
		configs := make(map[string]aws.Config)
		for i := range targets {
			id := targets[i].CredentialsAccountID()
			if _, ok := configs[id]; !ok {
				configs[id], err = loadAWSConfigForCredentialsAccount(ctx, cfg, id, awsFlags.CredentialsFile)
				if err != nil {
					return err
				}
			}
			targets[i].AWSConfig = configs[id]
		}
	}
	query.Targets = targets
	result := extendedsupport.NewCostResult(query)
	if len(targets) > 0 && hasBillingRegions {
		status.Step("Fetching Extended Support charges from Cost Explorer…")
		result, err = fetchExtendedSupportCosts(ctx, query)
		if err != nil {
			return err
		}
	}
	if len(sel.OUs) > 0 || sel.TagKey != "" {
		result.Notices = append(result.Notices, "OU/tag account selection reflects current Organizations membership/tags, not historical membership during the billing period.")
	}
	if query.TotalsOnly {
		notice := "Defaulting to totals for the last 90 reliable Cost Explorer days (90 complete UTC days with a two-day billing-update buffer)."
		if f.period.excludeRecentDays != 2 {
			notice = fmt.Sprintf("Defaulting to totals for the last 90 complete Cost Explorer days, excluding the %d most recent complete UTC days.", f.period.excludeRecentDays)
		}
		result.Notices = append(result.Notices, notice)
	}
	out, closeOut, err := resolveCommandOutput(cmd, f.output)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}
	format, err := output.ParseFormat(f.format)
	if err != nil {
		return err
	}
	return output.WriteExtendedSupportCostResult(out, format, result)
}

func resolveExtendedSupportCostTargetsImpl(cmd *cobra.Command, cfg configstore.File, sel *costTargetSelector, status costStepper) ([]cost.AccountTarget, error) {
	mode, err := validateCostTargetSelector(*sel)
	if err != nil {
		return nil, err
	}
	if mode == costTargetModeOrg {
		payers, err := resolveSelectorPayers(cfg, sel.PayerAliases)
		if err != nil {
			return nil, err
		}
		targets := make([]cost.AccountTarget, 0, len(payers))
		for _, payer := range payers {
			targets = append(targets, cost.AccountTarget{AccountID: payer.AccountID, DisplayAlias: payer.Alias})
		}
		return targets, nil
	}
	targets, err := resolveCostTargets(cmd, cfg, sel, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod, status)
	if err != nil {
		return nil, err
	}
	for i := range targets {
		targets[i].ScopeAccountOnly = true
	}
	return targets, nil
}
