package report

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	corehcp "github.com/openshift-online/finops-tools/core/hcphierarchy"
)

// Stepper reports long-running steps while generating a report.
type Stepper interface {
	Step(message string)
}

// GenerateInput is shared context for building and rendering a report template.
type GenerateInput struct {
	Format         string
	Out            io.Writer
	Targets        []cost.AccountTarget
	Range          cost.DateRange
	Progress       Stepper
	Now            time.Time
	ConfigPath     string
	SnowflakeAlias string
	// SnowflakeProdAlias selects the Telesense/prod Snowflake account alias
	// for reports needing the second connection (spec section 3.2/4.1),
	// e.g. executive-summary. Empty for reports that only need the
	// sandbox/mapping connection (SnowflakeAlias).
	SnowflakeProdAlias string
	Workers            int
	SelectionRootID    string

	// MappingCSVPath is the path to the executive-summary report's required
	// external account-taxonomy mapping CSV (golang-executive-summary-spec.md
	// section 3.4). Empty for reports that don't use it.
	MappingCSVPath string
	// InfraAccountIDs is the executive-summary report's shared-infrastructure
	// AWS account ID list (spec 6.3's DEFAULT_INFRA_ACCOUNTS), supplied via
	// configuration/flags rather than hardcoded in source (spec 4.2).
	InfraAccountIDs []string
	// NoAnomalies skips the executive-summary report's ML and statistical
	// anomaly detection/fetch (spec 7.4), e.g. to speed up iteration.
	NoAnomalies bool
}

// SnowflakeMartOpener opens a Snowflake connection for mart-backed reports.
// Registered from cli/cmd at init to avoid an import cycle with Snowflake auth.
// snowflakeAlias is empty to use the configured default (snowflake.account_alias).
type SnowflakeMartOpener func(ctx context.Context, cfgPath, snowflakeAlias string) (corehcp.SnowflakeQueryer, error)

var snowflakeMartOpener SnowflakeMartOpener

// SetSnowflakeMartOpener registers the CLI Snowflake opener for mart-backed reports.
func SetSnowflakeMartOpener(fn SnowflakeMartOpener) {
	snowflakeMartOpener = fn
}

// SnowflakeTelesenseOpener opens a second, independent Snowflake connection
// for reports that need vCPU-hour usage data from
// TELESENSE_DB.OPENSHIFT_MARTS.OCP_USAGE (golang-executive-summary-spec.md
// section 3.2), e.g. the executive-summary report. The return type is
// corehcp.SnowflakeQueryer because core/telesense.SnowflakeQueryer has an
// identical method set over the same core/sqlrows.Rows alias, so callers
// can pass the result straight to telesense.Build without an adapter. This
// connection is never SQL-joined to the SnowflakeMartOpener connection --
// they live in separate Snowflake accounts (spec sections 2, 11.1).
//
// Registration (SetSnowflakeTelesenseOpener, mirroring
// SetSnowflakeMartOpener) is added alongside the first generator that
// consumes it, to avoid an unused-registration window between this type's
// introduction and its first real caller.
type SnowflakeTelesenseOpener func(ctx context.Context, cfgPath, snowflakeAlias string) (corehcp.SnowflakeQueryer, error)

var snowflakeTelesenseOpener SnowflakeTelesenseOpener

// SetSnowflakeTelesenseOpener registers the CLI Snowflake opener for the
// Telesense/prod connection (executive-summary report only).
func SetSnowflakeTelesenseOpener(fn SnowflakeTelesenseOpener) {
	snowflakeTelesenseOpener = fn
}

// Generator builds and renders one report template.
type Generator interface {
	Validate(in GenerateInput) error
	Generate(ctx context.Context, in GenerateInput) error
}

// GeneratorFor returns the generator registered for a parsed template name.
func GeneratorFor(name string) (Generator, error) {
	if name == TemplateHCPHierarchy {
		return newHCPHierarchyGenerator(snowflakeMartOpener), nil
	}
	if name == TemplateExecutiveSummary {
		return newExecutiveSummaryGenerator(snowflakeMartOpener, snowflakeTelesenseOpener), nil
	}
	g, ok := generators[name]
	if !ok {
		return nil, fmt.Errorf("unsupported template %q", name)
	}
	return g, nil
}

var generators = map[string]Generator{
	TemplateCosts:         costsGenerator{},
	TemplateSavingsPlans:  savingsPlansGenerator{},
	TemplateCostAnomalies: costAnomaliesGenerator{},
}

// AccountTargetMode describes whether a report uses AWS account targeting flags.
type AccountTargetMode int

const (
	// AccountTargetsRequired needs --account-id/--account-alias, --ou, --tag, or --payer.
	AccountTargetsRequired AccountTargetMode = iota
	// AccountTargetsSnowflake uses --snowflake-alias, not AWS targets.
	AccountTargetsSnowflake
	// AccountTargetsRequiredAndSnowflake needs both AWS account targets
	// (e.g. --account-alias rh-control,osd-staging-1,osd-staging-2) and
	// Snowflake (executive-summary only, which joins both in memory).
	AccountTargetsRequiredAndSnowflake
)

// AccountTargetModeFor returns how a template uses account targeting flags.
func AccountTargetModeFor(templateName string) AccountTargetMode {
	switch templateName {
	case TemplateHCPHierarchy:
		return AccountTargetsSnowflake
	case TemplateExecutiveSummary:
		return AccountTargetsRequiredAndSnowflake
	default:
		return AccountTargetsRequired
	}
}

func validateTemplateFormat(templateName, format string) error {
	for _, t := range Templates() {
		if t.Name != templateName {
			continue
		}
		for _, f := range t.Formats {
			if f == format {
				return nil
			}
		}
		return fmt.Errorf("template %q does not support format %q", templateName, format)
	}
	return fmt.Errorf("unsupported template %q", templateName)
}
