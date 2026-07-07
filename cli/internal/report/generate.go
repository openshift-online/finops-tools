package report

import (
	"context"
	"fmt"
	"io"
	"time"

	corehcp "github.com/openshift-online/finops-tools/core/hcphierarchy"
	"github.com/openshift-online/finops-tools/core/cost"
)

// Stepper reports long-running steps while generating a report.
type Stepper interface {
	Step(message string)
}

// GenerateInput is shared context for building and rendering a report template.
type GenerateInput struct {
	Format     string
	Out        io.Writer
	Targets    []cost.AccountTarget
	Range      cost.DateRange
	Progress   Stepper
	Now        time.Time
	ConfigPath     string
	SnowflakeAlias string
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
	// AccountTargetsRequired needs --account/--account-alias, --ou, or --tag-key.
	AccountTargetsRequired AccountTargetMode = iota
	// AccountTargetsSnowflake uses --snowflake-alias, not AWS targets.
	AccountTargetsSnowflake
)

// AccountTargetModeFor returns how a template uses account targeting flags.
func AccountTargetModeFor(templateName string) AccountTargetMode {
	switch templateName {
	case TemplateHCPHierarchy:
		return AccountTargetsSnowflake
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
