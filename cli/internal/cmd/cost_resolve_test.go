package cmd

import (
	"testing"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/core/cost"
)

func TestValidateCostTargetSelectorOU(t *testing.T) {
	if _, err := validateCostTargetSelector(costTargetSelector{}); err == nil {
		t.Fatal("expected error when no selector provided")
	}
	if _, err := validateCostTargetSelector(costTargetSelector{OUs: []configstore.OUSelector{{ID: "ou-abcd-12345678"}}}); err == nil {
		t.Fatal("expected error when --ou without --payer")
	}
	mode, err := validateCostTargetSelector(costTargetSelector{PayerAlias: "rh-control"})
	if err != nil {
		t.Fatalf("payer alone should select org: %v", err)
	}
	if mode != costTargetModeOrg {
		t.Fatalf("mode = %v, want org", mode)
	}
	if _, err := validateCostTargetSelector(costTargetSelector{
		OUs:        []configstore.OUSelector{{ID: "ou-abcd-12345678"}},
		PayerAlias: "rh-control",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeCostTargets(t *testing.T) {
	merged := mergeCostTargets(
		[]cost.AccountTarget{{AccountID: "111111111111", DisplayAlias: "a"}},
		[]cost.AccountTarget{{AccountID: "111111111111", DisplayAlias: "b"}},
	)
	if len(merged) != 1 {
		t.Fatalf("expected deduped target, got %+v", merged)
	}
	if merged[0].DisplayAlias != "a" {
		t.Fatalf("expected alias from first segment, got %q", merged[0].DisplayAlias)
	}

	merged = mergeCostTargets(
		[]cost.AccountTarget{{AccountID: "222222222222"}},
		[]cost.AccountTarget{{AccountID: "222222222222", DisplayAlias: "linked"}},
	)
	if merged[0].DisplayAlias != "linked" {
		t.Fatalf("expected alias fill-in from second segment, got %q", merged[0].DisplayAlias)
	}
}
