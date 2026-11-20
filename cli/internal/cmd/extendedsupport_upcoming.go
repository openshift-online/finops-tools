// extendedsupport_upcoming.go implements "finops extended-support upcoming".
package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	"github.com/openshift-online/finops-tools/core/awshealth"
	"github.com/openshift-online/finops-tools/core/awsregions"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/spf13/cobra"
)

var (
	fetchExtendedSupportUpcoming             = awshealth.FetchUpcoming
	ensureExtendedSupportUpcomingCredentials = ensureCostCredentialsImpl
	extendedSupportUpcomingNow               = time.Now
)

type extendedSupportUpcomingFlags struct {
	payer, service, services, regions, format, output string
	skipRegions                                       []string
	quiet                                             bool
	workers                                           int
	requestInterval                                   time.Duration
}

func init() {
	extendedSupportCmd.AddCommand(newExtendedSupportUpcomingCommand())
}

func newExtendedSupportUpcomingCommand() *cobra.Command {
	f := &extendedSupportUpcomingFlags{}
	cmd := &cobra.Command{
		Use:   "upcoming",
		Short: "List upcoming Extended Support lifecycle events and affected resources",
		Long: `List future organizational AWS Health events containing "planned lifecycle event"
in their description or normalized event type code, and "extended support" in their
description. Text matching ignores case and repeated whitespace. Events are ordered
by effective start time, with affected accounts and resource IDs/ARNs.

--payer accepts registered payer aliases, comma-separated. This command queries
Health directly across all services by default. --service filters lifecycle events
by AWS Health service code (for example rds or rds,eks,lambda); opensearch maps to ES.
--services is an alias; use one spelling per command.
--regions/--skip-regions filter event regions; use global for global events.
All reported resource statuses, including RESOLVED and missing statuses, are retained.
Output formats: pretty-print (default), JSON, CSV. Use --output to save a report.

Health requests, including retries, share a minimum interval of 100ms per
payer client. --health-request-interval adjusts this client pacing policy; it is
not an AWS-published quota. SDK adaptive retries back off when Health throttles.
--workers bounds concurrent event collection while sharing this request pacing.

Payer credentials need enabled Health organizational access, an eligible AWS Support
plan, health:DescribeEventsForOrganization, health:DescribeAffectedAccountsForOrganization,
health:DescribeEventDetailsForOrganization, health:DescribeAffectedEntitiesForOrganization,
and organizations:ListAccounts. Individual lookup failures remain visible as warnings
and incomplete results. Events without a confirmed matching description are omitted.
No billing queries or member resource scans are performed.`,
		Example: `  finops extended-support upcoming --payer rh-control
  finops extended-support upcoming --payer rh-control,osd-staging-1 --format json
  finops extended-support upcoming --payer rh-control --service rds
  finops extended-support upcoming --payer rh-control --service rds,eks,lambda
  finops extended-support upcoming --payer rh-control --health-request-interval 2s
  finops extended-support upcoming --payer rh-control --format csv -o upcoming.csv`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			_, _, err := f.query(cmd)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return f.run(cmd) },
	}
	cmd.Flags().StringVar(&f.payer, "payer", "", "Registered payer alias(es), comma-separated (required); queries organizational Health directly")
	cmd.Flags().StringVar(&f.service, "service", "", "Filter lifecycle events by Health service code(s), comma-separated (default: all; opensearch maps to ES)")
	cmd.Flags().StringVar(&f.services, "services", "", "Alias for --service")
	cmd.Flags().StringVar(&f.regions, "regions", "", "Limit events to comma-separated AWS regions, including global (default: all event regions)")
	bindSkipRegionsFlag(cmd, &f.skipRegions)
	cmd.Flags().Lookup("skip-regions").Usage = "Exclude event regions, including global (comma-separated or repeatable; overrides --regions)"
	cmd.Flags().DurationVar(&f.requestInterval, "health-request-interval", awshealth.DefaultRequestInterval,
		"Minimum interval between Health request attempts per payer client, including retries")
	cmd.Flags().StringVar(&f.format, "format", string(output.FormatPrettyPrint), "Output format: pretty-print, json, csv")
	addOutputFlag(cmd, &f.output)
	cmd.Flags().BoolVar(&f.quiet, "quiet", false, "Suppress progress messages on stderr")
	bindWorkersFlag(cmd, &f.workers, "")
	cmd.Flags().Lookup("workers").Usage = "Maximum concurrent event collectors sharing request pacing (use 1 for sequential)"
	return cmd
}

func (f *extendedSupportUpcomingFlags) query(cmd *cobra.Command) (awshealth.Query, []string, error) {
	q := awshealth.Query{Now: extendedSupportUpcomingNow().UTC(), Workers: f.workers, RequestInterval: f.requestInterval}
	if q.RequestInterval <= 0 {
		return q, nil, fmt.Errorf("--health-request-interval must be positive")
	}
	aliases, err := configstore.ParsePayerAliases(f.payer)
	if err != nil {
		return q, nil, fmt.Errorf("--payer: %w", err)
	}
	if _, err := output.ParseFormat(f.format); err != nil {
		return q, nil, err
	}
	if err := validateWorkers(q.Workers); err != nil {
		return q, nil, err
	}
	if q.Regions, err = extendedsupport.ParseRegions(f.regions); err != nil {
		return q, nil, err
	}
	if q.SkipRegions, err = skipRegionsForCommand(cmd, f.skipRegions); err != nil {
		return q, nil, err
	}
	serviceFlag, services := "services", f.services
	if cmd.Flags().Changed("service") {
		if cmd.Flags().Changed("services") {
			return q, nil, fmt.Errorf("--service and --services cannot be used together")
		}
		serviceFlag, services = "service", f.service
	}
	if cmd.Flags().Changed(serviceFlag) {
		q.Services = awshealth.NormalizeServices(strings.Split(services, ","))
		if len(q.Services) == 0 {
			return q, nil, fmt.Errorf("--%s requires at least one Health service code", serviceFlag)
		}
	}
	return q, aliases, nil
}

func (f *extendedSupportUpcomingFlags) run(cmd *cobra.Command) error {
	q, aliases, err := f.query(cmd)
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
	payers, err := resolveSelectorPayers(cfg, aliases)
	if err != nil {
		return err
	}
	status := progress.New(cmd.ErrOrStderr(), f.quiet)
	ctx := awsCommandContext(cmd)
	targets := make([]cost.AccountTarget, len(payers))
	for i, payer := range payers {
		targets[i] = cost.AccountTarget{AccountID: payer.AccountID}
	}
	hasRegions := len(q.Regions) == 0 || len(awsregions.Exclude(q.Regions, q.SkipRegions)) > 0
	if hasRegions {
		status.Step("Ensuring AWS Health organization credentials…")
		if err := ensureExtendedSupportUpcomingCredentials(ctx, cmd, cfg, targets, awsFlags.ConfigPath, awsFlags.CredentialsFile, awsFlags.AuthMethod); err != nil {
			return err
		}
	}
	for _, target := range targets {
		if hasRegions {
			target.AWSConfig, err = loadAWSConfigForCredentialsAccount(ctx, cfg, target.AccountID, awsFlags.CredentialsFile)
			if err != nil {
				return err
			}
		}
		q.Targets = append(q.Targets, awshealth.Target{AccountID: target.AccountID, AWSConfig: target.AWSConfig})
	}
	status.Step("Fetching upcoming Extended Support lifecycle events and affected resources…")
	result, err := fetchExtendedSupportUpcoming(ctx, q)
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "warning: payer %s event %s account %s (%s): %s\n", output.SanitizeTerminal(warning.PayerAccountID), output.SanitizeTerminal(warning.EventARN), output.SanitizeTerminal(warning.AccountID), output.SanitizeTerminal(warning.Operation), output.SanitizeTerminal(warning.Message)); err != nil {
			return err
		}
	}
	format, err := output.ParseFormat(f.format)
	if err != nil {
		return err
	}
	out, closeOut, err := resolveCommandOutput(cmd, f.output)
	if err != nil {
		return err
	}
	if closeOut != nil {
		defer closeOut()
	}
	return output.WriteUpcomingLifecycleResult(out, format, result)
}
