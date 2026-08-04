package report

import (
	"context"
	"fmt"

	coreca "github.com/openshift-online/finops-tools/core/report/costanomalies"
)

type costAnomaliesGenerator struct{}

func (costAnomaliesGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateCostAnomalies, in.Format); err != nil {
		return err
	}
	if len(in.Targets) == 0 {
		return fmt.Errorf("cost-anomalies report requires an account target (--account-id/--account-alias, --ou, --tag, or --payer)")
	}
	return nil
}

func (costAnomaliesGenerator) Generate(ctx context.Context, in GenerateInput) error {
	if len(in.Targets) > 1 {
		in.Progress.Step(fmt.Sprintf("Fetching cost anomalies for %d account(s) from AWS Cost Anomaly Detection…", len(in.Targets)))
	} else {
		in.Progress.Step("Fetching cost anomalies from AWS Cost Anomaly Detection…")
	}
	caReport, err := coreca.Build(ctx, in.Targets, in.Range)
	if err != nil {
		return err
	}
	in.Progress.Step("Rendering HTML report…")
	return RenderCostAnomaliesHTML(in.Out, caReport, FormatAccountSummary(in.Targets))
}
