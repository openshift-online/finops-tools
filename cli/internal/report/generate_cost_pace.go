package report

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/report/costpace"
)

type costPaceGenerator struct {
	build func(context.Context, cost.CostQuery, time.Time) (costpace.Report, error)
}

func (costPaceGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateCostPace, in.Format); err != nil {
		return err
	}
	if len(in.Targets) == 0 {
		return fmt.Errorf("cost-pace report requires an account target (--account-alias, --account-id, --ou, --tag, or --payer)")
	}
	return nil
}

func (g costPaceGenerator) Generate(ctx context.Context, in GenerateInput) error {
	if err := g.Validate(in); err != nil {
		return err
	}
	if in.Progress != nil {
		in.Progress.Step("Fetching month-to-date and previous-month net amortized costs…")
	}
	build := g.build
	if build == nil {
		build = costpace.Build
	}
	r, err := build(ctx, cost.CostQuery{
		Provider: cost.ProviderAWS, Accounts: in.Targets, Range: in.Range,
		Workers: in.Workers, Progress: in.Progress,
	}, in.Now)
	if err != nil {
		return err
	}
	return RenderCostPaceHTML(in.Out, r, FormatAccountSummary(in.Targets))
}
