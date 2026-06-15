package report

import (
	"context"
	"fmt"

	corehcp "github.com/openshift-online/finops-tools/core/hcphierarchy"
)

type hcpHierarchyGenerator struct {
	opener SnowflakeMartOpener
}

func newHCPHierarchyGenerator(opener SnowflakeMartOpener) hcpHierarchyGenerator {
	return hcpHierarchyGenerator{opener: opener}
}

func (g hcpHierarchyGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateHCPHierarchy, in.Format); err != nil {
		return err
	}
	if g.opener == nil {
		return fmt.Errorf("hcp-hierarchy report: snowflake opener not configured")
	}
	return nil
}

func (g hcpHierarchyGenerator) Generate(ctx context.Context, in GenerateInput) error {
	sf, err := g.opener(ctx, in.ConfigPath, in.SnowflakeAlias)
	if err != nil {
		return err
	}
	defer func() { _ = sf.Close() }()
	in.Progress.Step("Resolving HCP hierarchy from Snowflake mart…")
	hcpReport, err := corehcp.Build(ctx, sf, "")
	if err != nil {
		return err
	}
	in.Progress.Step("Rendering HTML report…")
	return RenderHCPHierarchyHTML(in.Out, hcpReport)
}
