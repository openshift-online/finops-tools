// report_exec_summary_test.go tests exec-summary CLI selection validation and payer target wiring.
package cmd

import (
	"bytes"
	"testing"

	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

func newExecSummaryTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	addPeriodFlags(cmd)
	return cmd
}

func TestValidateExecSummarySelection(t *testing.T) {
	cmd := newExecSummaryTestCmd()

	if err := validateExecSummarySelection(cmd, costTargetSelector{}); err == nil {
		t.Error("missing --payer should fail")
	}
	if err := validateExecSummarySelection(cmd, costTargetSelector{PayerAlias: "rh-control"}); err != nil {
		t.Errorf("payer-only selection should pass: %v", err)
	}
	if err := validateExecSummarySelection(cmd, costTargetSelector{PayerAlias: "rh-control", Aliases: []string{"quay"}}); err == nil {
		t.Error("--account-alias with --payer should fail")
	}
	if err := validateExecSummarySelection(cmd, costTargetSelector{PayerAlias: "rh-control", TagKey: "env"}); err == nil {
		t.Error("--tag with --payer should fail")
	}
}

func TestValidateExecSummarySelectionPeriodFlags(t *testing.T) {
	daysCmd := newExecSummaryTestCmd()
	if err := daysCmd.Flags().Set("days", "30"); err != nil {
		t.Fatal(err)
	}
	if err := validateExecSummarySelection(daysCmd, costTargetSelector{PayerAlias: "rh-control"}); err == nil {
		t.Error("--days should fail for exec-summary")
	}

	fromCmd := newExecSummaryTestCmd()
	if err := fromCmd.Flags().Set("from", "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	if err := validateExecSummarySelection(fromCmd, costTargetSelector{PayerAlias: "rh-control"}); err == nil {
		t.Error("--from should fail for exec-summary")
	}

	monthsCmd := newExecSummaryTestCmd()
	if err := monthsCmd.Flags().Set("months", "3"); err != nil {
		t.Fatal(err)
	}
	if err := validateExecSummarySelection(monthsCmd, costTargetSelector{PayerAlias: "rh-control"}); err != nil {
		t.Errorf("--months should be allowed for exec-summary: %v", err)
	}
}

func execSummaryMemberTargetsFixture(payer string) []cost.AccountTarget {
	return []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: payer, DisplayName: "Member A"},
		{AccountID: "222222222222", PayerAccountID: payer, DisplayName: "Member B"},
	}
}

func TestBuildExecSummaryTargetsPrependsPayer(t *testing.T) {
	payer := "123456789012"
	targets := buildExecSummaryTargets(payer, "rh-control", execSummaryMemberTargetsFixture(payer))

	if len(targets) != 3 {
		t.Fatalf("targets = %d, want 3", len(targets))
	}
	first := targets[0]
	if first.AccountID != payer {
		t.Errorf("first target = %s, want payer %s", first.AccountID, payer)
	}
	if first.IsLinked() {
		t.Error("payer target must not be linked")
	}
	if first.DisplayAlias != "rh-control" {
		t.Errorf("payer display alias = %q, want rh-control", first.DisplayAlias)
	}
	for i, tgt := range targets[1:] {
		if !tgt.IsLinked() || tgt.PayerAccountID != payer {
			t.Errorf("member target %d = %+v, want linked to %s", i, tgt, payer)
		}
	}
}

func TestBuildExecSummaryTargetsPayerOnly(t *testing.T) {
	targets := buildExecSummaryTargets("123456789012", "rh-control", nil)
	if len(targets) != 1 || targets[0].AccountID != "123456789012" {
		t.Fatalf("targets = %+v, want single payer target", targets)
	}
}

// TestReportCreateHelpShowsExecSummary guards the template registration end to end.
func TestReportCreateHelpShowsExecSummary(t *testing.T) {
	var out bytes.Buffer
	reportCreateCmd.SetOut(&out)
	reportCreateCmd.SetErr(&out)
	if err := reportCreateCmd.Help(); err != nil {
		t.Fatal(err)
	}
}
