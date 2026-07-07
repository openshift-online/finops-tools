package report

import (
	"testing"

	"github.com/openshift-online/finops-tools/core/cost"
)

func TestGeneratorForKnownTemplates(t *testing.T) {
	for _, name := range []string{TemplateCosts, TemplateSavingsPlans, TemplateCostAnomalies, TemplateHCPHierarchy} {
		if _, err := GeneratorFor(name); err != nil {
			t.Fatalf("GeneratorFor(%q): %v", name, err)
		}
	}
}

func TestCostAnomaliesGeneratorRequiresTargets(t *testing.T) {
	gen, err := GeneratorFor(TemplateCostAnomalies)
	if err != nil {
		t.Fatal(err)
	}
	err = gen.Validate(GenerateInput{Format: FormatHTML})
	if err == nil {
		t.Fatal("expected error for zero targets")
	}
}

func TestSavingsPlansGeneratorRequiresTargets(t *testing.T) {
	gen, err := GeneratorFor(TemplateSavingsPlans)
	if err != nil {
		t.Fatal(err)
	}
	err = gen.Validate(GenerateInput{Format: FormatHTML})
	if err == nil {
		t.Fatal("expected error for zero targets")
	}
}

func TestCostsGeneratorRequiresTargets(t *testing.T) {
	gen, err := GeneratorFor(TemplateCosts)
	if err != nil {
		t.Fatal(err)
	}
	err = gen.Validate(GenerateInput{Format: FormatHTML})
	if err == nil {
		t.Fatal("expected error for zero targets")
	}
}

func TestAccountTargetModeFor(t *testing.T) {
	if got := AccountTargetModeFor(TemplateHCPHierarchy); got != AccountTargetsSnowflake {
		t.Fatalf("hcp-hierarchy mode = %v, want snowflake", got)
	}
	if got := AccountTargetModeFor(TemplateCosts); got != AccountTargetsRequired {
		t.Fatalf("costs mode = %v, want required", got)
	}
	if got := AccountTargetModeFor(TemplateSavingsPlans); got != AccountTargetsRequired {
		t.Fatalf("savings-plans mode = %v, want required", got)
	}
}

func TestHCPHierarchyGeneratorRequiresSnowflakeOpener(t *testing.T) {
	gen := newHCPHierarchyGenerator(nil)
	err := gen.Validate(GenerateInput{Format: FormatHTML})
	if err == nil {
		t.Fatal("expected error when snowflake opener is unset")
	}
}

func TestGeneratorValidateRejectsUnsupportedFormat(t *testing.T) {
	gen, err := GeneratorFor(TemplateCosts)
	if err != nil {
		t.Fatal(err)
	}
	err = gen.Validate(GenerateInput{
		Format:  "pdf",
		Targets: []cost.AccountTarget{{AccountID: "111111111111"}},
	})
	if err == nil {
		t.Fatal("expected format error")
	}
}
