package report

import (
	"context"
	"fmt"

	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	corereport "github.com/openshift-online/finops-tools/core/report"
)

type costsGenerator struct{}

func (costsGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateCosts, in.Format); err != nil {
		return err
	}
	if len(in.Targets) == 0 {
		return fmt.Errorf("costs report requires an account target (--account-alias, --account, --ou, or --tag-key)")
	}
	return nil
}

func (costsGenerator) Generate(ctx context.Context, in GenerateInput) error {
	if len(in.Targets) == 0 {
		return fmt.Errorf("costs report requires an account target (--account-alias, --account, --ou, or --tag-key)")
	}

	if len(in.Targets) > 1 {
		in.Progress.Step(fmt.Sprintf("Fetching net amortized costs for %d account(s) from AWS Cost Explorer…", len(in.Targets)))
	}

	costQuery := cost.CostQuery{
		Provider: cost.ProviderAWS,
		Accounts: in.Targets,
		Range:    in.Range,
		Progress: in.Progress,
		AWSFetch: &cost.AWSFetchOptions{
			ResolveAccountNames: coreaccount.ResolveAccountNames,
		},
	}

	report, err := corereport.BuildCostsReport(ctx, costQuery, in.Progress)
	if err != nil {
		return err
	}
	in.Progress.Step("Rendering HTML report…")
	return RenderCostsHTML(in.Out, report)
}
