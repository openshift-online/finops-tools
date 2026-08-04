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
	reportGenerateAccount         string
	reportGenerateAccountAliases  string
	reportGenerateFormat          string
	reportGenerateOU              string
	reportGenerateOutput          string
	reportGeneratePayer           string
	reportGenerateQuiet           bool
	reportGenerateTag             string
	reportGenerateSkipOrgCache    bool
	reportGenerateRefreshOrgCache bool
	reportCreateSnowflakeAlias    string
	reportGenerateWorkers         int
)

var reportCreateCmd = &cobra.Command{
	Use:   "create [template]",
	Short: "Create a report from a template",
	Long: `Create a report for configured cloud accounts.

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
			reportGenerateSkipOrgCache, reportGenerateRefreshOrgCache,
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
	if err := applyCostPeriodDefaults(cmd, cfg); err != nil {
		return err
	}

	status := progress.New(cmd.ErrOrStderr(), reportGenerateQuiet)

	sel, err := parseCostTargetSelector(
		reportGenerateAccount, reportGenerateAccountAliases, reportGenerateOU, reportGeneratePayer,
		reportGenerateTag,
		reportGenerateSkipOrgCache, reportGenerateRefreshOrgCache,
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
		Format:          format,
		Targets:         targets,
		Range:           dateRange,
		Progress:        status,
		Now:             time.Now().UTC(),
		ConfigPath:      cfgPath,
		SnowflakeAlias:  snowflakeAlias,
		Workers:         reportGenerateWorkers,
		SelectionRootID: sel.SelectionRootID,
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
