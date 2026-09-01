// period_flags.go registers shared cost query period flags and resolves them with config defaults.
package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

type periodFlagValues struct {
	days              int
	months            int
	from              string
	to                string
	excludeRecentDays int
}

var periodFlags periodFlagValues

func addPeriodFlags(cmd *cobra.Command) {
	cmd.Flags().IntVar(&periodFlags.days, "days", 0, "Last N calendar days (mutually exclusive with --months and --from/--to)")
	cmd.Flags().IntVar(&periodFlags.months, "months", 0, "Last N calendar months from the 1st of the month (mutually exclusive with --days and --from/--to)")
	cmd.Flags().StringVar(&periodFlags.from, "from", "", "Start date YYYY-MM-DD inclusive (optional --to)")
	cmd.Flags().StringVar(&periodFlags.to, "to", "", "End date YYYY-MM-DD inclusive (requires --from)")
	cmd.Flags().IntVar(&periodFlags.excludeRecentDays, "exclude-recent-days", 0,
		"Omit the last N UTC days from the end anchor (AWS CE lag; default 0, or defaults.cost.exclude_recent_days)")
}

func validatePeriodFlags(cmd *cobra.Command) error {
	if cmd.Flags().Changed("days") && periodFlags.days <= 0 {
		return fmt.Errorf("--days must be a positive integer")
	}
	if cmd.Flags().Changed("months") && periodFlags.months <= 0 {
		return fmt.Errorf("--months must be a positive integer")
	}
	if cmd.Flags().Changed("exclude-recent-days") && periodFlags.excludeRecentDays < 0 {
		return fmt.Errorf("--exclude-recent-days must be >= 0")
	}
	spec := periodSpecFromFlags()
	if err := validatePeriodSpecModes(spec); err != nil {
		return err
	}
	return nil
}

func applyExcludeRecentDaysDefault(cmd *cobra.Command, cfg configstore.File, dest *int) error {
	if cmd.Flags().Changed("exclude-recent-days") {
		return nil
	}
	if v, ok := cfg.Default(configstore.DefaultFQNCostExcludeRecentDays); ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 {
			return fmt.Errorf("defaults.%s: invalid value %q", configstore.DefaultFQNCostExcludeRecentDays, v)
		}
		*dest = n
	}
	return nil
}

func applyCostPeriodDefaults(cmd *cobra.Command, cfg configstore.File) error {
	if err := cfg.ValidateCostPeriodDefaults(); err != nil {
		return err
	}
	if !cmd.Flags().Changed("days") {
		if v, ok := cfg.Default(configstore.DefaultFQNCostDays); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n <= 0 {
				return fmt.Errorf("defaults.%s: invalid value %q", configstore.DefaultFQNCostDays, v)
			}
			periodFlags.days = n
		}
	}
	if !cmd.Flags().Changed("months") {
		if v, ok := cfg.Default(configstore.DefaultFQNCostMonths); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n <= 0 {
				return fmt.Errorf("defaults.%s: invalid value %q", configstore.DefaultFQNCostMonths, v)
			}
			periodFlags.months = n
		}
	}
	if !cmd.Flags().Changed("from") {
		if v, ok := cfg.Default(configstore.DefaultFQNCostFrom); ok {
			periodFlags.from = v
		}
	}
	if !cmd.Flags().Changed("to") {
		if v, ok := cfg.Default(configstore.DefaultFQNCostTo); ok {
			periodFlags.to = v
		}
	}
	if !cmd.Flags().Changed("exclude-recent-days") {
		if v, ok := cfg.Default(configstore.DefaultFQNCostExcludeRecentDays); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 {
				return fmt.Errorf("defaults.%s: invalid value %q", configstore.DefaultFQNCostExcludeRecentDays, v)
			}
			periodFlags.excludeRecentDays = n
		}
	}
	return validatePeriodSpecModes(periodSpecFromFlags())
}

func periodSpecFromFlags() cost.PeriodSpec {
	return cost.PeriodSpec{
		Days:              periodFlags.days,
		Months:            periodFlags.months,
		From:              periodFlags.from,
		To:                periodFlags.to,
		ExcludeRecentDays: periodFlags.excludeRecentDays,
	}
}

func validatePeriodSpecModes(spec cost.PeriodSpec) error {
	fromSet := strings.TrimSpace(spec.From) != ""
	toSet := strings.TrimSpace(spec.To) != ""
	if toSet && !fromSet {
		return fmt.Errorf("--to requires --from")
	}
	modes := 0
	if spec.Days > 0 {
		modes++
	}
	if spec.Months > 0 {
		modes++
	}
	if fromSet || toSet {
		modes++
	}
	if modes > 1 {
		return fmt.Errorf("only one period mode allowed (use one of --days, --months, or --from/--to)")
	}
	if (spec.Days > 0 || spec.Months > 0) && (fromSet || toSet) {
		return fmt.Errorf("cannot combine --days or --months with --from/--to")
	}
	return nil
}

func resolveCostPeriod(now time.Time) (cost.DateRange, error) {
	return cost.ResolvePeriod(periodSpecFromFlags(), now)
}
