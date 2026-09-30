// report_create.go implements "finops report create".
package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	reportpkg "github.com/openshift-online/finops-tools/cli/internal/report"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

var (
	reportGenerateAccount          string
	reportGenerateAccountAliases   string
	reportGenerateFormat           string
	reportGenerateOU               string
	reportGenerateOutput           string
	reportGeneratePayer            string
	reportGenerateQuiet            bool
	reportGenerateTag              string
	reportGenerateSkipOrgCache     bool
	reportGenerateRefreshOrgCache  bool
	reportCreateSnowflakeAlias     string
	reportCreateSnowflakeProdAlias string
	reportGenerateWorkers          int
	reportCreateMappingCSV         string
	reportCreateInfraAccountIDs    string
	reportCreateNoAnomalies        bool
)

var reportCreateCmd = &cobra.Command{
	Use:   "create [template]",
	Short: "Create a report from a template",
	Long: `Create a report for configured cloud accounts.

AWS account selection matches finops account get-cost (--ou/--payer select ACTIVE
members only).

Example:
  finops report list
  finops report create costs --account-alias rh-control
  finops report create costs --account-alias rh-control -o costs.html
  finops report create costs --account-id 333333333333 --payer rhc -o member.html
  finops report create costs --ou ou-abcd-12345678 --payer rh-control -o ou-costs.html
  finops report create costs --payer rh-control -o all-members.html
  finops report create costs --payer rh-control --tag env=prod -o prod.html
  finops report create hcp-hierarchy --snowflake-alias rhsandbox -o hcp-hierarchy.html`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		templateName, err := reportpkg.ParseTemplate(args[0])
		if err != nil {
			return err
		}
		sel, err := parseCostTargetSelector(
			reportGenerateAccount, reportGenerateAccountAliases, reportGenerateOU, reportGeneratePayer,
			reportGenerateTag,
			reportGenerateSkipOrgCache, reportGenerateRefreshOrgCache, false,
		)
		if err != nil {
			return err
		}
		if err := validateReportCostTargetSelector(templateName, sel, reportCreateSnowflakeAlias); err != nil {
			return err
		}
		if err := validatePeriodFlags(cmd); err != nil {
			return err
		}
		if _, err := reportpkg.ParseFormat(reportGenerateFormat); err != nil {
			return err
		}
		if err := validateWorkers(reportGenerateWorkers); err != nil {
			return err
		}
		return validateOrgCacheFlags(reportGenerateSkipOrgCache, reportGenerateRefreshOrgCache)
	},
	RunE: runReportCreate,
}

func init() {
	reportCmd.AddCommand(reportCreateCmd)
	bindAWSTargetFlags(reportCreateCmd, awsTargetFlagRefs{
		Account:         &reportGenerateAccount,
		AccountAliases:  &reportGenerateAccountAliases,
		OU:              &reportGenerateOU,
		Payer:           &reportGeneratePayer,
		Tag:             &reportGenerateTag,
		SkipOrgCache:    &reportGenerateSkipOrgCache,
		RefreshOrgCache: &reportGenerateRefreshOrgCache,
	})
	reportCreateCmd.Flags().StringVar(&reportGenerateFormat, "format", reportpkg.FormatHTML, "Output format (supported: html)")
	reportCreateCmd.Flags().StringVar(&reportCreateSnowflakeAlias, "snowflake-alias", "", "Snowflake account alias for Snowflake-backed reports")
	reportCreateCmd.Flags().StringVar(&reportCreateSnowflakeProdAlias, "snowflake-prod-alias", "", "Snowflake account alias for the Telesense/prod connection (executive-summary template only)")
	reportCreateCmd.Flags().StringVar(&reportCreateMappingCSV, "mapping-csv", "", "Path to the account-taxonomy mapping CSV (executive-summary template only, required)")
	reportCreateCmd.Flags().StringVar(&reportCreateInfraAccountIDs, "infra-account-ids", "", "Comma-separated shared-infrastructure AWS account IDs (executive-summary template only, required)")
	reportCreateCmd.Flags().BoolVar(&reportCreateNoAnomalies, "no-anomalies", false, "Skip ML and statistical anomaly detection (executive-summary template only)")
	addOutputFlag(reportCreateCmd, &reportGenerateOutput)
	reportCreateCmd.Flags().BoolVar(&reportGenerateQuiet, "quiet", false, "Suppress progress messages on stderr")
	bindWorkersFlag(reportCreateCmd, &reportGenerateWorkers, "costs template only; ")
	addPeriodFlags(reportCreateCmd)
}

func runReportCreate(cmd *cobra.Command, args []string) error {
	templateName, err := reportpkg.ParseTemplate(args[0])
	if err != nil {
		return err
	}
	format, err := reportpkg.ParseFormat(reportGenerateFormat)
	if err != nil {
		return err
	}
	gen, err := reportpkg.GeneratorFor(templateName)
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
	applyExecutiveSummaryMonthsDefault(cmd, templateName)
	if err := applyCostPeriodDefaults(cmd, cfg); err != nil {
		return err
	}

	status := progress.New(cmd.ErrOrStderr(), reportGenerateQuiet)

	sel, err := parseCostTargetSelector(
		reportGenerateAccount, reportGenerateAccountAliases, reportGenerateOU, reportGeneratePayer,
		reportGenerateTag,
		reportGenerateSkipOrgCache, reportGenerateRefreshOrgCache, false,
	)
	if err != nil {
		return err
	}

	var targets []cost.AccountTarget
	var snowflakeAlias string
	targetMode := reportpkg.AccountTargetModeFor(templateName)
	switch targetMode {
	case reportpkg.AccountTargetsSnowflake:
		snowflakeAlias = strings.TrimSpace(reportCreateSnowflakeAlias)
	case reportpkg.AccountTargetsRequiredAndSnowflake:
		snowflakeAlias = strings.TrimSpace(reportCreateSnowflakeAlias)
		targets, err = resolveCostTargets(
			cmd, cfg, &sel,
			awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod,
			status,
		)
		if err != nil {
			return err
		}
	default:
		targets, err = resolveCostTargets(
			cmd, cfg, &sel,
			awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod,
			status,
		)
		if err != nil {
			return err
		}
	}

	dateRange, err := resolveCostPeriod(time.Now().UTC())
	if err != nil {
		return err
	}

	in := reportpkg.GenerateInput{
		Format:             format,
		Targets:            targets,
		Range:              dateRange,
		Progress:           status,
		Now:                time.Now().UTC(),
		ConfigPath:         cfgPath,
		SnowflakeAlias:     snowflakeAlias,
		SnowflakeProdAlias: strings.TrimSpace(reportCreateSnowflakeProdAlias),
		Workers:            reportGenerateWorkers,
		SelectionRootID:    sel.SelectionRootID,
		MappingCSVPath:     strings.TrimSpace(reportCreateMappingCSV),
		InfraAccountIDs:    parseInfraAccountIDs(reportCreateInfraAccountIDs),
		NoAnomalies:        reportCreateNoAnomalies,
	}
	if err := gen.Validate(in); err != nil {
		return err
	}

	reportCtx := cmd.Context()
	if len(targets) > 0 {
		reportCtx = awsCommandContext(cmd)
		status.Step("Ensuring AWS credentials…")
		if err := ensureCostCredentials(reportCtx, cmd, cfg, targets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
			return err
		}
		if len(targets) <= 1 {
			status.Step("Preparing account configuration…")
		}
		prepareBar := progress.NewBar(cmd.ErrOrStderr(), reportGenerateQuiet, "Preparing account configuration…", len(targets))
		targets, err = prepareCostTargets(reportCtx, cfg, targets, awsFlags.CredentialsFile, prepareBar)
		if err != nil {
			return err
		}
		in.Targets = targets
	}

	out, closeOut, err := resolveCommandOutput(cmd, reportGenerateOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}
	in.Out = out

	if err := gen.Generate(reportCtx, in); err != nil {
		return err
	}

	if !reportGenerateQuiet {
		if path := strings.TrimSpace(reportGenerateOutput); path != "" {
			status.Step(fmt.Sprintf("Wrote report to %s", path))
		} else {
			status.Step("Report written to stdout")
		}
	}
	return nil
}

// applyExecutiveSummaryMonthsDefault defaults --months to 5 for the
// executive-summary template (golang-executive-summary-spec.md's default
// reporting window) when the user hasn't selected any period mode
// explicitly. Applied before applyCostPeriodDefaults so a configured
// defaults.cost.months (or any other period flag) still takes precedence.
func applyExecutiveSummaryMonthsDefault(cmd *cobra.Command, templateName string) {
	if templateName != reportpkg.TemplateExecutiveSummary {
		return
	}
	if cmd.Flags().Changed("days") || cmd.Flags().Changed("months") ||
		cmd.Flags().Changed("from") || cmd.Flags().Changed("to") {
		return
	}
	periodFlags.months = 5
}

// parseInfraAccountIDs splits --infra-account-ids on commas, trimming
// whitespace and dropping empty entries.
func parseInfraAccountIDs(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
