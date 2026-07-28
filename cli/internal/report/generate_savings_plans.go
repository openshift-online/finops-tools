package report

import (
	"context"
	"fmt"

	coresp "github.com/openshift-online/finops-tools/core/report/savingsplans"
)

type savingsPlansGenerator struct{}

func (savingsPlansGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateSavingsPlans, in.Format); err != nil {
		return err
	}
	if len(in.Targets) == 0 {
		return fmt.Errorf("savings-plans report requires an account target (--account-id/--account-alias, --ou, --tag, or --payer)")
	}
	return nil
}

func (savingsPlansGenerator) Generate(ctx context.Context, in GenerateInput) error {
	if len(in.Targets) > 1 {
		in.Progress.Step(fmt.Sprintf("Fetching Savings Plans data for %d account(s) from AWS Cost Explorer…", len(in.Targets)))
	} else {
		in.Progress.Step("Fetching Savings Plans data from AWS Cost Explorer…")
	}
	spReport, err := coresp.Build(ctx, in.Targets, in.Range)
	if err != nil {
		return err
	}
	in.Progress.Step("Rendering HTML report…")
	return RenderSavingsPlansHTML(in.Out, spReport)
}
