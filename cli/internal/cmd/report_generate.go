// report_generate.go implements "finops report generate".
package cmd

import (
	"fmt"
	"os"
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
	reportGenerateOUDirect        bool
	reportGenerateOutput          string
	reportGeneratePayer           string
	reportGenerateQuiet           bool
	reportGenerateTagKey          string
	reportGenerateTagValue        string
	reportGenerateSkipOrgCache    bool
	reportGenerateRefreshOrgCache bool
)

var reportGenerateCmd = &cobra.Command{
	Use:   "generate [template]",
	Short: "Generate a report from a template",
	Long: `Generate a report for configured cloud accounts.

Example:
  finops report list
  finops report generate costs --account-alias rh-control
  finops report generate costs --account-alias rh-control -o costs.html
  finops report generate costs --account 333333333333 --payer rhc -o member.html
  finops report generate costs --ou ou-abcd-1234 --payer rh-control -o ou-costs.html
  finops report generate costs --payer rh-control --tag-key env --tag-value prod -o prod.html
  finops report generate hcp-hierarchy --account-alias rhsandbox -o hcp-hierarchy.html`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		templateName, err := reportpkg.ParseTemplate(args[0])
		if err != nil {
			return err
		}
		sel, err := parseCostTargetSelector(
			reportGenerateAccount, reportGenerateAccountAliases, reportGenerateOU, reportGeneratePayer,
			reportGenerateTagKey, reportGenerateTagValue, reportGenerateOUDirect,
			reportGenerateSkipOrgCache, reportGenerateRefreshOrgCache,
		)
		if err != nil {
			return err
		}
		if err := validateReportCostTargetSelector(templateName, sel); err != nil {
			return err
		}
		if err := validatePeriodFlags(cmd); err != nil {
			return err
		}
		if _, err := reportpkg.ParseFormat(reportGenerateFormat); err != nil {
			return err
		}
		return validateOrgCacheFlags(reportGenerateSkipOrgCache, reportGenerateRefreshOrgCache)
	},
	RunE: runReportGenerate,
}

func init() {
	reportCmd.AddCommand(reportGenerateCmd)
	reportGenerateCmd.Flags().StringVar(&reportGenerateFormat, "format", reportpkg.FormatHTML, "Output format (supported: html)")
	reportGenerateCmd.Flags().StringVar(&reportGenerateAccount, "account", "", "Payer AWS account ID(s), comma-separated 12-digit IDs")
	reportGenerateCmd.Flags().StringVar(&reportGenerateAccountAliases, "account-alias", "", "Account alias: comma-separated AWS aliases for AWS reports, or a single Snowflake alias for Snowflake-backed reports")
	reportGenerateCmd.Flags().StringVar(&reportGenerateOU, "ou", "", "AWS OU ID(s), comma-separated (requires --payer; recursive by default)")
	reportGenerateCmd.Flags().BoolVar(&reportGenerateOUDirect, "ou-direct", false, "Include only accounts directly in --ou, not descendant OUs")
	reportGenerateCmd.Flags().StringVar(&reportGeneratePayer, "payer", "", "Registered payer alias for --account member IDs, --ou, or --tag-key (e.g. rhc)")
	reportGenerateCmd.Flags().StringVar(&reportGenerateTagKey, "tag-key", "", "Select accounts by AWS Organizations tag key")
	reportGenerateCmd.Flags().StringVar(&reportGenerateTagValue, "tag-value", "", "Optional tag value (omit to match any value for --tag-key)")
	reportGenerateCmd.Flags().StringVarP(&reportGenerateOutput, "output", "o", "", "Write HTML to this file instead of stdout")
	reportGenerateCmd.Flags().BoolVar(&reportGenerateQuiet, "quiet", false, "Suppress progress messages on stderr")
	reportGenerateCmd.Flags().BoolVar(&reportGenerateSkipOrgCache, "skip-org-cache", false, "Bypass cached organization account/tag data (always fetch live from AWS)")
	reportGenerateCmd.Flags().BoolVar(&reportGenerateRefreshOrgCache, "refresh-org-cache", false, "Ignore cached organization data and refresh the cache from AWS")
	addPeriodFlags(reportGenerateCmd)
}

func runReportGenerate(cmd *cobra.Command, args []string) error {
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
		reportGenerateTagKey, reportGenerateTagValue, reportGenerateOUDirect,
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
		if len(sel.Aliases) == 1 {
			snowflakeAlias = sel.Aliases[0]
		}
	default:
		if targetMode != reportpkg.AccountTargetsOptional || costTargetSelectorSpecified(sel) {
			targets, err = resolveCostTargets(
				cmd.Context(), cmd, cfg, sel,
				awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod,
				status,
			)
			if err != nil {
				return err
			}
		}
	}

	dateRange, err := resolveCostPeriod(time.Now().UTC())
	if err != nil {
		return err
	}

	in := reportpkg.GenerateInput{
		Format:         format,
		Targets:        targets,
		Range:          dateRange,
		Progress:       status,
		Now:            time.Now().UTC(),
		ConfigPath:     cfgPath,
		SnowflakeAlias: snowflakeAlias,
	}
	if err := gen.Validate(in); err != nil {
		return err
	}

	if len(targets) > 0 {
		status.Step("Ensuring AWS credentials…")
		if err := ensureCostCredentials(cmd.Context(), cmd, cfg, targets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
			return err
		}
		if len(targets) <= 1 {
			status.Step("Preparing account configuration…")
		}
		targets, err = prepareCostTargets(cmd.Context(), cfg, targets, awsFlags.CredentialsFile, status)
		if err != nil {
			return err
		}
		in.Targets = targets
	}

	out, closeOut, err := openReportGenerateOutput(reportGenerateOutput)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}
	in.Out = out

	if err := gen.Generate(cmd.Context(), in); err != nil {
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

func openReportGenerateOutput(path string) (*os.File, func(), error) {
	if path = strings.TrimSpace(path); path != "" {
		f, err := os.Create(path)
		if err != nil {
			return nil, nil, fmt.Errorf("create output file: %w", err)
		}
		return f, func() { _ = f.Close() }, nil
	}
	return os.Stdout, nil, nil
}
