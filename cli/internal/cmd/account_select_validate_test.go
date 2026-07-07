package cmd

import (
	"testing"

	"github.com/openshift-online/finops-tools/core/cost"
)

func TestValidateAccountSelectorOU(t *testing.T) {
	if _, err := validateAccountSelector(accountSelector{}); err == nil {
		t.Fatal("expected error when no selector provided")
	}
	if _, err := validateAccountSelector(accountSelector{OUIDs: []string{"ou-abcd-1234"}}); err == nil {
		t.Fatal("expected error when --ou without --payer")
	}
	if _, err := validateAccountSelector(accountSelector{OUDirectOnly: true}); err == nil {
		t.Fatal("expected error when --ou-direct without --ou")
	}
	if _, err := validateAccountSelector(accountSelector{PayerAlias: "rh-control"}); err == nil {
		t.Fatal("expected error when --payer without --account or --ou")
	}
	if _, err := validateAccountSelector(accountSelector{
		OUIDs:      []string{"ou-abcd-1234"},
		PayerAlias: "rh-control",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeAccounts(t *testing.T) {
	merged := mergeAccounts(
		[]cost.AccountTarget{{AccountID: "111111111111", DisplayAlias: "a"}},
		[]cost.AccountTarget{{AccountID: "111111111111", DisplayAlias: "b"}},
	)
	if len(merged) != 1 {
		t.Fatalf("expected deduped target, got %+v", merged)
	}
	if merged[0].DisplayAlias != "a" {
		t.Fatalf("expected alias from first segment, got %q", merged[0].DisplayAlias)
	}

	merged = mergeAccounts(
		[]cost.AccountTarget{{AccountID: "222222222222"}},
		[]cost.AccountTarget{{AccountID: "222222222222", DisplayAlias: "linked"}},
	)
	if merged[0].DisplayAlias != "linked" {
		t.Fatalf("expected alias fill-in from second segment, got %q", merged[0].DisplayAlias)
	}
}
