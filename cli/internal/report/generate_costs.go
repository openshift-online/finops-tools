package report

import (
	"context"
	"fmt"
	"strings"

	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	corereport "github.com/openshift-online/finops-tools/core/report"
)

var (
	reportOrganizationRootID    = coreaccount.OrganizationRootID
	reportBuildOUAccountMapping = coreaccount.BuildOUAccountMapping
)

type costsGenerator struct{}

func (costsGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateCosts, in.Format); err != nil {
		return err
	}
	if len(in.Targets) == 0 {
		return fmt.Errorf("costs report requires an account target (--account-alias, --account-id, --ou, --tag, or --payer)")
	}
	return nil
}

func (costsGenerator) Generate(ctx context.Context, in GenerateInput) error {
	if len(in.Targets) == 0 {
		return fmt.Errorf("costs report requires an account target (--account-alias, --account-id, --ou, --tag, or --payer)")
	}

	if len(in.Targets) > 1 && in.Progress != nil {
		in.Progress.Step(fmt.Sprintf("Fetching net amortized costs for %d account(s) from AWS Cost Explorer…", len(in.Targets)))
	}

	awsFetch := &cost.AWSFetchOptions{
		ResolveAccountNames: coreaccount.ResolveAccountNames,
	}
	if in.Progress != nil {
		in.Progress.Step("Mapping accounts to organizational units…")
	}
	buckets, hierarchy, err := resolveReportOUMapping(ctx, in)
	if err != nil {
		return fmt.Errorf("map accounts to organizational units: %w", err)
	}
	if len(buckets) > 0 {
		awsFetch.AccountOUBuckets = buckets
		awsFetch.OUHierarchy = hierarchy
	}

	costQuery := cost.CostQuery{
		Provider: cost.ProviderAWS,
		Accounts: in.Targets,
		Range:    in.Range,
		Progress: in.Progress,
		Workers:  in.Workers,
		AWSFetch: awsFetch,
	}

	report, err := corereport.BuildCostsReport(ctx, costQuery, in.Progress)
	if err != nil {
		return err
	}
	if in.Progress != nil {
		in.Progress.Step("Rendering HTML report…")
	}
	return RenderCostsHTML(in.Out, report)
}

func resolveReportOUMapping(ctx context.Context, in GenerateInput) (map[string]cost.OUBucket, []cost.OUHierarchyNode, error) {
	if len(in.Targets) == 0 {
		return nil, nil, nil
	}

	groups := cost.GroupByCredentialsAccount(in.Targets)
	useSelectionRoot := len(groups) == 1 && strings.TrimSpace(in.SelectionRootID) != ""

	out := make(map[string]cost.OUBucket)
	var nodes []cost.OUHierarchyNode

	for _, credID := range cost.SortedCredentialAccountIDs(groups) {
		group := groups[credID]
		cfg := group[0].AWSConfig
		rootID := ""
		if useSelectionRoot {
			rootID = strings.TrimSpace(in.SelectionRootID)
		}
		if rootID == "" {
			var err error
			rootID, err = reportOrganizationRootID(ctx, cfg)
			if err != nil {
				return nil, nil, fmt.Errorf("resolve organization root for payer %s: %w", credID, err)
			}
		}
		ids := make([]string, 0, len(group))
		for _, t := range group {
			ids = append(ids, t.AccountID)
		}
		mapped, hierarchy, err := reportBuildOUAccountMapping(ctx, cfg, rootID, ids)
		if err != nil {
			return nil, nil, fmt.Errorf("map accounts to OUs for payer %s: %w", credID, err)
		}
		for accountID, bucket := range mapped {
			out[accountID] = cost.OUBucket{ID: bucket.ID, Name: bucket.Name}
		}
		for _, n := range hierarchy {
			nodes = append(nodes, cost.OUHierarchyNode{
				ID:       n.ID,
				Name:     n.Name,
				ParentID: n.ParentID,
				Depth:    n.Depth,
			})
		}
	}
	return out, nodes, nil
}