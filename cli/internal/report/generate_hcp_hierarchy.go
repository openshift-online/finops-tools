package report

import (
	"context"
	"fmt"

	corehcp "github.com/openshift-online/finops-tools/core/hcphierarchy"
)

type hcpHierarchyGenerator struct{}

func (hcpHierarchyGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateHCPHierarchy, in.Format); err != nil {
		return err
	}
	if snowflakeMartOpener == nil {
		return fmt.Errorf("hcp-hierarchy report: snowflake opener not configured")
	}
	return nil
}

func (hcpHierarchyGenerator) Generate(ctx context.Context, in GenerateInput) error {
	sf, err := snowflakeMartOpener(ctx, in.ConfigPath, in.SnowflakeAlias)
	if err != nil {
		return err
	}
	in.Progress.Step("Resolving HCP hierarchy from Snowflake mart…")
	hcpReport, err := corehcp.Build(ctx, sf, "")
	if err != nil {
		return err
	}
	in.Progress.Step("Rendering HTML report…")
	return RenderHCPHierarchyHTML(in.Out, hcpReport)
}
