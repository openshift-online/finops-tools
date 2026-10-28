package report

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
)

// execSummaryPayerDef pairs one of the executive-summary report's registered
// AWS account aliases (golang-executive-summary-spec.md section 4, "no new
// AWS targeting flags needed") with its spec section 7 payer tab identity.
// These are internal finops aliases, not AWS account IDs.
//
// Per the PR48 breakup design's amendment 3, only rh-control (Production) is
// registered until --payer accepts multiple master payers; the multi-payer
// structure (PayerID, per-payer maps, combine functions) lands as designed
// with a single payer registered, so adding payers later is a registration
// change, not a refactor.
type execSummaryPayerDef struct {
	Alias string
	ID    execsum.PayerID
	Label string
}

var execSummaryPayerDefs = []execSummaryPayerDef{
	{Alias: "rh-control", ID: execsum.PayerProduction, Label: "Production"},
}

type executiveSummaryGenerator struct {
	martOpener SnowflakeMartOpener
}

func newExecutiveSummaryGenerator(martOpener SnowflakeMartOpener) executiveSummaryGenerator {
	return executiveSummaryGenerator{martOpener: martOpener}
}

func (g executiveSummaryGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateExecutiveSummary, in.Format); err != nil {
		return err
	}
	if g.martOpener == nil {
		return fmt.Errorf("executive-summary report: snowflake mart opener not configured")
	}
	if strings.TrimSpace(in.SnowflakeAlias) == "" {
		return fmt.Errorf("executive-summary report requires --snowflake-alias")
	}
	if strings.TrimSpace(in.MappingCSVPath) == "" {
		return fmt.Errorf("executive-summary report requires --mapping-csv")
	}
	if _, err := execSummaryPayerTargets(in.Targets); err != nil {
		return err
	}
	window := execSummaryWindow(in.Range)
	if len(window.Months) < 2 {
		return fmt.Errorf(
			"executive-summary report needs at least 2 months in the reporting window (got %d); widen --months",
			len(window.Months),
		)
	}
	return nil
}

// Generate runs the executive-summary report's pipeline. Merge 0 (this
// skeleton) computes only the reporting window and writes the minimal JSON
// data dump via --data-dump-out; data fetching, analytics, and HTML
// rendering land in later merges of the breakup series.
func (g executiveSummaryGenerator) Generate(ctx context.Context, in GenerateInput) error {
	if _, err := execSummaryPayerTargets(in.Targets); err != nil {
		return err
	}
	window := execSummaryWindow(in.Range)
	if len(window.Months) < 2 {
		return fmt.Errorf(
			"executive-summary report needs at least 2 months in the reporting window (got %d); widen --months",
			len(window.Months),
		)
	}

	path := strings.TrimSpace(in.DataDumpOutPath)
	if path == "" {
		return fmt.Errorf("executive-summary report requires --data-dump-out until HTML rendering lands in a later merge")
	}

	in.Progress.Step("Writing full data dump JSON to " + path + "…")
	dump := FullDataDump{
		GeneratedAt: in.Now.Format(time.RFC3339),
		WindowStart: execsum.MonthLabel(window.Start),
		WindowEnd:   execsum.MonthLabel(window.End),
		Months:      monthLabelsFor(window.Months),
		DataSources: []string{},
	}
	if err := WriteFullDataDumpJSON(path, dump); err != nil {
		return fmt.Errorf("write full data dump: %w", err)
	}
	return nil
}

// execSummaryPayerTargets resolves the report's registered AWS payer aliases
// against the resolved cost targets by DisplayAlias.
func execSummaryPayerTargets(targets []cost.AccountTarget) (map[execsum.PayerID]cost.AccountTarget, error) {
	byAlias := make(map[string]cost.AccountTarget, len(targets))
	for _, t := range targets {
		if alias := strings.TrimSpace(t.DisplayAlias); alias != "" {
			byAlias[alias] = t
		}
	}

	out := make(map[execsum.PayerID]cost.AccountTarget, len(execSummaryPayerDefs))
	var missing []string
	for _, pd := range execSummaryPayerDefs {
		t, ok := byAlias[pd.Alias]
		if !ok {
			missing = append(missing, pd.Alias)
			continue
		}
		out[pd.ID] = t
	}
	if len(missing) > 0 {
		names := make([]string, len(execSummaryPayerDefs))
		for i, pd := range execSummaryPayerDefs {
			names[i] = pd.Alias
		}
		return nil, fmt.Errorf(
			"executive-summary report requires --account-alias %s (missing: %s)",
			strings.Join(names, ","), strings.Join(missing, ", "),
		)
	}
	return out, nil
}

// execSummaryWindow converts the CLI's cost.DateRange into the report's
// whole-month TimeWindow: every calendar month touched by the range, keyed
// by first-of-month, ending at the month before dr.End's month.
func execSummaryWindow(dr cost.DateRange) execsum.TimeWindow {
	lastIncluded := dr.End.AddDate(0, 0, -1)
	months := execsum.MonthsInWindow(dr.Start, lastIncluded)
	if len(months) == 0 {
		return execsum.TimeWindow{}
	}
	return execsum.TimeWindow{Start: months[0], End: months[len(months)-1], Months: months}
}

// monthLabelsFor formats each month as a YYYY-MM key.
func monthLabelsFor(months []time.Time) []string {
	out := make([]string, len(months))
	for i, m := range months {
		out[i] = execsum.MonthLabel(m)
	}
	return out
}
