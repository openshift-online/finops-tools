// account_details.go implements "finops account details".
package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/notify"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	"github.com/openshift-online/finops-tools/core/accountreview"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/ocmmapping"
	"github.com/spf13/cobra"
)

var (
	detailsAccount           string
	detailsAccountAliases    string
	detailsFormat            string
	detailsGroupBy           string
	detailsMonths            int
	detailsExcludeRecentDays int
	detailsOU                string
	detailsOutput            string
	detailsPayer             string
	detailsQuiet             bool
	detailsRedirectPrefix    string
	detailsRole              string
	detailsSend              bool
	detailsSkipOrgCache      bool
	detailsRefreshOrgCache   bool
	detailsSnowflakeAlias    string
	detailsTag               string
	detailsWorkers           int
	detailsYes               bool
	accountReviewBuild       = accountreview.Build
)

var accountDetailsCmd = &cobra.Command{
	Use:   "details",
	Short: "Show AWS account cost and inventory details, optionally email the owner",
	Long: `Gather AWS account cost trends and resource inventory for one or more accounts.

The same details are printed (--format pretty-print, json, or csv) and used in
owner notification emails. The owner email is derived from the Organizations
account tag (default key: owner), appending @redhat.com when the tag value has
no @ sign.

Account selection matches finops account get-cost: --account-id, --account-alias,
--ou, --tag, or --payer alone. Linked member accounts are scanned using role
assumption from the payer.

By default the command prints details and does not send email. To email owners
via Gmail, pass --send with either --yes (owner addresses) or --redirect-prefix
(PREFIX+<owner>@redhat.com test delivery). --group-by applies only to email
batching; stdout is always one record per account.

A Cost Explorer, inventory, or owner-tag failure on one account is recorded for
that account and does not stop the rest of the run. Incomplete inventory (including
assume-role failures) is shown as an inventory warning and still included in owner
email with a generic incomplete-scan note.

OpenShift clusters are loaded from the production Dataverse Snowflake mart
(HCMFINOPS_DB.MARTS.OCM_MAPPING) across Production, Stage, and Integration.
Use --snowflake-alias to select the Snowflake account (default:
snowflake.account_alias). A blank, unknown, or incomplete --snowflake-alias fails
before AWS work. When no Snowflake account is configured (and the flag is omitted)
or the lookup fails after connect, AWS cost/inventory still succeed and the
OpenShift section records a soft error.

Gmail uses gcloud Application Default Credentials (finops does not modify ADC). Verify access:
  finops config gmail login

Examples:
  finops account details --account-alias my-linked
  finops account details --payer rh-control --ou 'ou-abcd-12345678/*' --format json -o review.json
  finops account details --account-alias my-linked --snowflake-alias rhprod
  finops account details --account-alias my-linked --send --redirect-prefix finops
  finops account details --payer rh-control --ou ou-abcd-12345678 --group-by owner --send --yes`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		sel, err := parseCostTargetSelector(
			detailsAccount, detailsAccountAliases, detailsOU, detailsPayer,
			detailsTag,
			detailsSkipOrgCache, detailsRefreshOrgCache,
		)
		if err != nil {
			return err
		}
		if _, err := validateCostTargetSelector(sel); err != nil {
			return err
		}
		if _, err := accountreview.ParseGroupBy(detailsGroupBy); err != nil {
			return err
		}
		if _, err := output.ParseFormat(detailsFormat); err != nil {
			return err
		}
		if detailsMonths <= 0 {
			return fmt.Errorf("--months must be positive")
		}
		if detailsExcludeRecentDays < 0 {
			return fmt.Errorf("--exclude-recent-days must be >= 0")
		}
		if err := validateWorkers(detailsWorkers); err != nil {
			return err
		}
		if err := validateOrgCacheFlags(detailsSkipOrgCache, detailsRefreshOrgCache); err != nil {
			return err
		}
		return validateAccountDetailsSendFlags(
			detailsSend, detailsYes, detailsRedirectPrefix, cmd.Flags().Changed("group-by"),
		)
	},
	RunE: runAccountDetails,
}

func init() {
	accountCmd.AddCommand(accountDetailsCmd)
	bindAWSTargetFlags(accountDetailsCmd, awsTargetFlagRefs{
		Account:         &detailsAccount,
		AccountAliases:  &detailsAccountAliases,
		OU:              &detailsOU,
		Payer:           &detailsPayer,
		Tag:             &detailsTag,
		SkipOrgCache:    &detailsSkipOrgCache,
		RefreshOrgCache: &detailsRefreshOrgCache,
	})
	accountDetailsCmd.Flags().BoolVar(&detailsSend, "send", false, "Send owner notification emails via Gmail after printing details")
	accountDetailsCmd.Flags().BoolVar(&detailsYes, "yes", false, "With --send, deliver to resolved owner emails")
	accountDetailsCmd.Flags().StringVar(&detailsRedirectPrefix, "redirect-prefix", "",
		"With --send, deliver to PREFIX+<owner>@redhat.com instead of owner addresses")
	accountDetailsCmd.Flags().StringVar(&detailsGroupBy, "group-by", string(accountreview.GroupByAccount), "Group emails by account or owner (send only)")
	accountDetailsCmd.Flags().IntVar(&detailsMonths, "months", 6, "Number of calendar months of cost history to include")
	accountDetailsCmd.Flags().IntVar(&detailsExcludeRecentDays, "exclude-recent-days", 0,
		"Omit the last N UTC days from the cost end anchor (AWS CE lag; default 0, or defaults.cost.exclude_recent_days)")
	accountDetailsCmd.Flags().StringVar(&detailsFormat, "format", string(output.FormatPrettyPrint), "Output format: pretty-print, json, csv")
	addOutputFlag(accountDetailsCmd, &detailsOutput)
	bindLinkedRoleFlag(accountDetailsCmd, &detailsRole)
	accountDetailsCmd.Flags().StringVar(&detailsSnowflakeAlias, "snowflake-alias", "",
		"Snowflake account alias for OpenShift cluster lookup (default: snowflake.account_alias)")
	accountDetailsCmd.Flags().BoolVar(&detailsQuiet, "quiet", false, "Suppress progress messages on stderr")
	bindWorkersFlag(accountDetailsCmd, &detailsWorkers, "")
}

// validateAccountDetailsSendFlags requires an explicit delivery choice with --send
// so a dry-run cannot accidentally email owners.
func validateAccountDetailsSendFlags(send, yes bool, redirectPrefix string, groupBySet bool) error {
	redirectPrefix = strings.TrimSpace(redirectPrefix)
	if redirectPrefix != "" {
		if err := notify.ValidateRedirectPrefix(redirectPrefix); err != nil {
			return err
		}
	}
	if yes && !send {
		return fmt.Errorf("--yes requires --send")
	}
	if redirectPrefix != "" && !send {
		return fmt.Errorf("--redirect-prefix requires --send")
	}
	if groupBySet && !send {
		return fmt.Errorf("--group-by requires --send")
	}
	if yes && redirectPrefix != "" {
		return fmt.Errorf("cannot use --yes with --redirect-prefix")
	}
	if !send {
		return nil
	}
	if yes || redirectPrefix != "" {
		return nil
	}
	return fmt.Errorf("refusing to send: pass --yes for owner addresses or --redirect-prefix PREFIX for test delivery")
}

func runAccountDetails(cmd *cobra.Command, _ []string) error {
	format, err := output.ParseFormat(detailsFormat)
	if err != nil {
		return err
	}
	groupBy, err := accountreview.ParseGroupBy(detailsGroupBy)
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
	if err := applyExcludeRecentDaysDefault(cmd, cfg, &detailsExcludeRecentDays); err != nil {
		return err
	}
	if err := validateAccountDetailsSnowflakeAlias(cfg, detailsSnowflakeAlias, cmd.Flags().Changed("snowflake-alias")); err != nil {
		return err
	}

	status := progress.New(cmd.ErrOrStderr(), detailsQuiet)
	sel, err := parseCostTargetSelector(
		detailsAccount, detailsAccountAliases, detailsOU, detailsPayer,
		detailsTag,
		detailsSkipOrgCache, detailsRefreshOrgCache,
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

	if len(costTargets) == 0 {
		if err := writeAccountDetailsOutput(cmd, format, nil); err != nil {
			return err
		}
		if !detailsSend {
			return nil
		}
		return writeAccountDetailsDeliverySummary(cmd.ErrOrStderr(), skippedFromEmptyTargets(costTargets))
	}

	status.Step("Ensuring AWS credentials…")
	awsCtx := awsCommandContext(cmd)
	if err := ensureAccountDetailsCredentials(cmd, cfg, costTargets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
		return err
	}

	prepareCostBar := progress.NewBar(cmd.ErrOrStderr(), detailsQuiet, "Preparing account configuration…", len(costTargets))
	costTargets, err = prepareCostTargets(awsCtx, cfg, costTargets, awsFlags.CredentialsFile, prepareCostBar)
	if err != nil {
		return err
	}

	prepareInvBar := progress.NewBar(cmd.ErrOrStderr(), detailsQuiet, "Preparing inventory access…", len(costTargets))
	invTargets, err := prepareAccountDetailsTargets(
		cmd, cfg, costTargets,
		awsFlags.CredentialsFile, awsFlags.ConfigPath, detailsRole,
		detailsWorkers,
		prepareInvBar,
	)
	if err != nil {
		return err
	}

	status.Step("Resolving OU paths…")
	ouPaths := map[string]string{}
	buckets, hierarchy, _, ouErr := resolveAccountOUBuckets(awsCtx, cfg, sel, costTargets, awsFlags.CredentialsFile)
	if ouErr != nil {
		status.Step("Continuing without OU paths: " + ouErr.Error())
	} else {
		ouPaths = cost.FormatAccountOUPaths(buckets, hierarchy)
	}

	status.Step("Building account review reports…")
	buildResult, err := accountReviewBuild(awsCtx, accountreview.BuildInput{
		CostTargets:       costTargets,
		InventoryTargets:  invTargets,
		Months:            detailsMonths,
		ExcludeRecentDays: detailsExcludeRecentDays,
		Workers:           detailsWorkers,
		OUPaths:           ouPaths,
		Progress:          status,
	})
	if err != nil {
		return err
	}

	enrichAccountDetailsOpenShift(awsCtx, cfg, detailsSnowflakeAlias, status, buildResult.Reports)

	details := accountreview.DetailsFromReports(buildResult.Reports)
	// Open -o only after gather finishes so a long/failed run does not leave an empty file.
	if err := writeAccountDetailsOutput(cmd, format, details); err != nil {
		return err
	}
	if !detailsSend {
		return nil
	}

	groups, deliveryResults := accountreview.GroupReports(buildResult.Reports, groupBy)

	var messages []notify.Message
	for _, group := range groups {
		groupDetails := accountreview.DetailsFromReports(group.Reports)
		if len(groupDetails) == 1 {
			messages = append(messages, notify.RenderAccountEmail(groupDetails[0]))
		} else {
			messages = append(messages, notify.RenderOwnerGroupEmail(group.OwnerEmail, groupDetails))
		}
	}

	redirectPrefix := strings.TrimSpace(detailsRedirectPrefix)
	if redirectPrefix != "" {
		messages, err = notify.ApplyRedirectPrefix(messages, redirectPrefix)
		if err != nil {
			return err
		}
	} else {
		messages = notify.ApplyOwnerRecipients(messages)
	}

	messageMeta := make([]struct {
		accountIDs []string
		ownerEmail string
	}, len(groups))
	for i, group := range groups {
		accountIDs := make([]string, len(group.Reports))
		for j, r := range group.Reports {
			accountIDs[j] = r.AccountID
		}
		messageMeta[i] = struct {
			accountIDs []string
			ownerEmail string
		}{accountIDs: accountIDs, ownerEmail: group.OwnerEmail}
	}

	if len(messages) > 0 {
		status.Step("Sending owner notification emails…")
		sender, err := notify.NewGmailSender(cmd.Context())
		if err != nil {
			return err
		}
		for i, msg := range messages {
			meta := messageMeta[i]
			if err := sender.Send(cmd.Context(), msg); err != nil {
				deliveryResults = append(deliveryResults, accountreview.DeliveryResult{
					AccountIDs: meta.accountIDs,
					OwnerEmail: msg.IntendedTo,
					Status:     accountreview.StatusSendFailed,
					Reason:     err.Error(),
				})
				continue
			}
			deliveryResults = append(deliveryResults, accountreview.DeliveryResult{
				AccountIDs: meta.accountIDs,
				OwnerEmail: msg.IntendedTo,
				Status:     accountreview.StatusSent,
				Reason:     sentDeliveryReason(msg),
			})
		}
	}

	return accountDetailsAfterSummary(writeAccountDetailsDeliverySummary(cmd.ErrOrStderr(), deliveryResults), deliveryResults)
}

// validateAccountDetailsSnowflakeAlias fails fast when --snowflake-alias is set but
// blank, unknown, or incomplete (missing account identifier or warehouse), so AWS
// gather does not run for a typo. Omitting the flag is allowed (OpenShift lookup may
// soft-skip later if no default is configured).
func validateAccountDetailsSnowflakeAlias(cfg configstore.File, snowflakeAlias string, aliasFlagSet bool) error {
	if strings.TrimSpace(snowflakeAlias) == "" {
		if aliasFlagSet {
			return fmt.Errorf("--snowflake-alias was set but is empty")
		}
		return nil
	}
	alias, acct, err := cfg.ResolveSnowflakeAccountAlias(snowflakeAlias)
	if err != nil {
		return err
	}
	if err := configstore.ValidateSnowflakeAccount(acct, alias); err != nil {
		return err
	}
	acct = cfg.ResolveSnowflakeSession(acct)
	return configstore.ValidateSnowflakeWarehouse(acct, alias)
}

// enrichAccountDetailsOpenShift loads OCM clusters from Snowflake and attaches them
// to reports. Soft-fails when Snowflake is unavailable so AWS review still completes.
// Callers must already validate an explicit --snowflake-alias via
// validateAccountDetailsSnowflakeAlias.
func enrichAccountDetailsOpenShift(
	ctx context.Context,
	cfg configstore.File,
	snowflakeAlias string,
	status *progress.Writer,
	reports []accountreview.AccountReport,
) {
	if len(reports) == 0 {
		return
	}
	_, _, resolveErr := cfg.ResolveSnowflakeAccountAlias(snowflakeAlias)
	if resolveErr != nil {
		status.Step("Skipping OpenShift clusters: " + resolveErr.Error())
		accountreview.ApplyOpenShiftClustersError(reports, resolveErr)
		return
	}

	status.Step("Looking up OpenShift clusters in Snowflake…")
	querier, err := openSnowflakeMartQuerier(ctx, cfg, snowflakeAlias)
	if err != nil {
		status.Step("Skipping OpenShift clusters: " + err.Error())
		accountreview.ApplyOpenShiftClustersError(reports, err)
		return
	}
	defer func() {
		_ = querier.Close()
	}()

	accountIDs := make([]string, len(reports))
	for i, r := range reports {
		accountIDs[i] = r.AccountID
	}
	byAccount, err := ocmmapping.LookupByAWSAccounts(ctx, querier, accountIDs, "")
	if err != nil {
		status.Step("Skipping OpenShift clusters: " + err.Error())
		accountreview.ApplyOpenShiftClustersError(reports, err)
		return
	}
	accountreview.ApplyOpenShiftClusters(reports, byAccount, nil)
}

// writeAccountDetailsOutput opens --output (if set) immediately before writing so
// long AWS gathers do not create an empty file that looks like a finished result.
func writeAccountDetailsOutput(cmd *cobra.Command, format output.Format, details []accountreview.AccountDetails) error {
	out, closer, err := resolveCommandOutput(cmd, detailsOutput)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer()
	}
	return output.WriteAccountDetails(out, format, details)
}

func sentDeliveryReason(msg notify.Message) string {
	if msg.DeliveryTo != "" && msg.IntendedTo != "" && msg.DeliveryTo != msg.IntendedTo {
		return fmt.Sprintf("sent to %s", msg.DeliveryTo)
	}
	return ""
}

func skippedFromEmptyTargets(targets []cost.AccountTarget) []accountreview.DeliveryResult {
	if len(targets) > 0 {
		return nil
	}
	return []accountreview.DeliveryResult{{
		Status: accountreview.StatusSkipped,
		Reason: "no accounts matched the selection",
	}}
}

// accountDetailsAfterSummary returns the summary-write error if set; otherwise a
// non-nil error when any result is StatusSendFailed so the command exits
// non-zero after the summary has already been written.
func accountDetailsAfterSummary(summaryErr error, results []accountreview.DeliveryResult) error {
	if summaryErr != nil {
		return summaryErr
	}
	n := 0
	for _, r := range results {
		if r.Status == accountreview.StatusSendFailed {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	if n == 1 {
		return fmt.Errorf("1 notification failed to send")
	}
	return fmt.Errorf("%d notifications failed to send", n)
}

func writeAccountDetailsDeliverySummary(w io.Writer, results []accountreview.DeliveryResult) error {
	planned := 0
	sent := 0
	failed := 0
	skipped := 0
	for _, r := range results {
		switch r.Status {
		case accountreview.StatusPlanned:
			planned++
		case accountreview.StatusSent:
			sent++
		case accountreview.StatusSkipped:
			skipped++
		case accountreview.StatusOwnerNotFound, accountreview.StatusInvalidOwner, accountreview.StatusSendFailed:
			failed++
		}
	}
	return output.WriteNotifySummary(w, output.FormatPrettyPrint, output.NotifySummary{
		Planned: planned,
		Sent:    sent,
		Failed:  failed,
		Skipped: skipped,
		Results: results,
	})
}
