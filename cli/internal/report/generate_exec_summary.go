// generate_exec_summary.go generates the single-payer AWS executive summary report.
package report

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/report/execsummary"
)

const (
	// execSummaryDefaultMonths is the default completed-month window.
	execSummaryDefaultMonths = 5
	// maxExecSummaryMonths caps --months so ComputeWindow cannot be asked to
	// allocate an unbounded month list before any Cost Explorer request.
	maxExecSummaryMonths = 120
)

// fetchMonthlyAccountService is stubbable for tests.
var fetchMonthlyAccountService = cost.FetchMonthlyAccountService

type execSummaryGenerator struct{}

func (execSummaryGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateExecSummary, in.Format); err != nil {
		return err
	}
	if in.Months > maxExecSummaryMonths {
		return fmt.Errorf("exec-summary report supports at most %d months, got %d", maxExecSummaryMonths, in.Months)
	}
	if len(in.Targets) == 0 {
		return fmt.Errorf("exec-summary report requires a payer target (--payer)")
	}
	return nil
}

func (execSummaryGenerator) Generate(ctx context.Context, in GenerateInput) error {
	months := in.Months
	if months <= 0 {
		months = execSummaryDefaultMonths
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	window, err := execsummary.ComputeWindow(months, &now)
	if err != nil {
		return err
	}
	// Cost Explorer half-open range covering the window's completed months.
	dr := cost.DateRange{Start: window.Start, End: window.End.AddDate(0, 1, 0)}

	if in.Progress != nil {
		in.Progress.Step(fmt.Sprintf("Fetching %d completed month(s) of account/service costs…", months))
	}
	res, err := fetchMonthlyAccountService(ctx, cost.CostQuery{
		Provider: cost.ProviderAWS,
		Accounts: in.Targets,
		Range:    dr,
		Progress: in.Progress,
		Workers:  in.Workers,
	})
	if err != nil {
		return err
	}

	payerAlias := ""
	for _, t := range in.Targets {
		if !t.IsLinked() {
			// Prefer the configured alias; DisplayName is the Organizations
			// account name, which is not what "payer alias" means in the report.
			payerAlias = t.DisplayAlias
			if payerAlias == "" {
				payerAlias = t.AccountDisplayName()
			}
			break
		}
	}

	names := make(map[string]string, len(in.Targets))
	for _, t := range in.Targets {
		names[t.AccountID] = t.AccountDisplayName()
	}

	rep, err := execsummary.BuildReport(payerAlias, res, window, names)
	if err != nil {
		return err
	}

	if in.Progress != nil {
		in.Progress.Step("Rendering HTML report…")
	}
	return RenderExecSummaryHTML(in.Out, rep)
}
