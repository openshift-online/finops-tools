package report

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
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

type noopStepper struct{}

func (noopStepper) Step(string) {}

func TestResolveReportOUMappingMultiPayer(t *testing.T) {
	origRoot := reportOrganizationRootID
	origBuild := reportBuildOUAccountMapping
	t.Cleanup(func() {
		reportOrganizationRootID = origRoot
		reportBuildOUAccountMapping = origBuild
	})

	rootCalls := make([]string, 0)
	reportOrganizationRootID = func(_ context.Context, cfg aws.Config) (string, error) {
		// Distinguish payers by a marker in Region (set on each target config).
		rootCalls = append(rootCalls, cfg.Region)
		switch cfg.Region {
		case "payer-a":
			return "r-aaaa", nil
		case "payer-b":
			return "r-bbbb", nil
		default:
			return "", fmt.Errorf("unexpected payer region %q", cfg.Region)
		}
	}
	reportBuildOUAccountMapping = func(_ context.Context, cfg aws.Config, rootID string, accountIDs []string) (map[string]coreaccount.AccountOUBucket, []coreaccount.OUHierarchyNode, error) {
		out := make(map[string]coreaccount.AccountOUBucket, len(accountIDs))
		for _, id := range accountIDs {
			out[id] = coreaccount.AccountOUBucket{ID: "ou-" + rootID, Name: "OU under " + rootID}
		}
		return out, []coreaccount.OUHierarchyNode{{ID: rootID, Name: "Root", Depth: 0}}, nil
	}

	buckets, hierarchy, err := resolveReportOUMapping(context.Background(), GenerateInput{
		Targets: []cost.AccountTarget{
			{AccountID: "111111111111", PayerAccountID: "123456789012", AWSConfig: aws.Config{Region: "payer-a"}},
			{AccountID: "222222222222", PayerAccountID: "987654321098", AWSConfig: aws.Config{Region: "payer-b"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rootCalls) != 2 {
		t.Fatalf("rootCalls = %v", rootCalls)
	}
	if buckets["111111111111"].ID != "ou-r-aaaa" || buckets["222222222222"].ID != "ou-r-bbbb" {
		t.Fatalf("buckets = %+v", buckets)
	}
	if len(hierarchy) != 2 {
		t.Fatalf("hierarchy = %+v", hierarchy)
	}
}

func TestCostsGenerateFailsOnOUMappingError(t *testing.T) {
	origRoot := reportOrganizationRootID
	t.Cleanup(func() {
		reportOrganizationRootID = origRoot
	})
	reportOrganizationRootID = func(context.Context, aws.Config) (string, error) {
		return "", fmt.Errorf("organizations access denied")
	}

	gen := costsGenerator{}
	err := gen.Generate(context.Background(), GenerateInput{
		Format:   FormatHTML,
		Progress: noopStepper{},
		Targets: []cost.AccountTarget{
			{AccountID: "111111111111", PayerAccountID: "123456789012", AWSConfig: aws.Config{}},
		},
	})
	if err == nil {
		t.Fatal("expected OU mapping error")
	}
	if !strings.Contains(err.Error(), "map accounts to organizational units") {
		t.Fatalf("error = %q", err.Error())
	}
	if !strings.Contains(err.Error(), "organizations access denied") {
		t.Fatalf("error = %q", err.Error())
	}
}
