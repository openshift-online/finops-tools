package cmd

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/openshift-online/finops-tools/cli/internal/aws"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/spf13/cobra"
)

func TestAWSMigrateAccountDryRun(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetAWSAlias("osd-staging-1", "987654321098")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetLinkedAccount("tenant-a", configstore.LinkedAccount{
		AccountID:  "111111111111",
		PayerAlias: "rh-control",
		Role:       "OrganizationAccountAccessRole",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := migrateAccountEnsureCredentials
	origLoad := migrateAccountLoadConfigForCreds
	origContains := migrateAccountContains
	origInvite := migrateAccountInvite
	defer func() {
		migrateAccountEnsureCredentials = origEnsure
		migrateAccountLoadConfigForCreds = origLoad
		migrateAccountContains = origContains
		migrateAccountInvite = origInvite
	}()

	migrateAccountEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	migrateAccountLoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	calls := 0
	migrateAccountContains = func(_ context.Context, _ aws.Config, accountID string) (bool, error) {
		calls++
		if accountID != "111111111111" {
			return false, nil
		}
		return calls == 1, nil
	}
	migrateAccountInvite = func(context.Context, aws.Config, string, string) (coreaccount.InviteResult, error) {
		t.Fatal("invite should not run on dry-run")
		return coreaccount.InviteResult{}, nil
	}

	migrateAccountIDFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "osd-staging-1"
	migrateAccountDestOU = ""
	migrateAccountRole = ""
	migrateAccountNotes = ""
	migrateAccountDryRun = true
	migrateAccountYes = false
	awsFlags.ConfigPath = configPath
	defer func() {
		migrateAccountIDFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("role", "", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runAWSMigrateAccount(cmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Dry run complete") {
		t.Fatalf("output = %q", got)
	}
	if !strings.Contains(got, "tenant-a (111111111111)") {
		t.Fatalf("missing account in plan: %q", got)
	}
	if !strings.Contains(got, "update role trust (to-payer)") {
		t.Fatalf("missing trust update step in plan: %q", got)
	}
}

func TestAWSMigrateAccountRequiresYes(t *testing.T) {
	migrateAccountIDFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "osd-staging-1"
	migrateAccountDryRun = false
	migrateAccountYes = false
	defer func() {
		migrateAccountIDFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
	}()

	err := awsMigrateAccountCmd.PreRunE(awsMigrateAccountCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}

func TestAWSMigrateAccountPayersMustDifferAfterTrim(t *testing.T) {
	migrateAccountIDFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "  rh-control  "
	migrateAccountDryRun = true
	migrateAccountYes = false
	defer func() {
		migrateAccountIDFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
	}()

	err := awsMigrateAccountCmd.PreRunE(awsMigrateAccountCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("expected must-differ error, got %v", err)
	}
}

func TestAWSMigrateAccountRejectsSamePayerAccountID(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetAWSAlias("rhc", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	migrateAccountIDFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "rhc"
	migrateAccountDryRun = true
	migrateAccountYes = false
	awsFlags.ConfigPath = configPath
	defer func() {
		migrateAccountIDFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("role", "", "")
	err = runAWSMigrateAccount(cmd, nil)
	if err == nil {
		t.Fatal("expected same-payer-ID error")
	}
	got := err.Error()
	if !strings.Contains(got, "rh-control") || !strings.Contains(got, "rhc") || !strings.Contains(got, "123456789012") {
		t.Fatalf("error should name both aliases and account ID, got %v", err)
	}
}

func TestFormatMigratePartialSummary(t *testing.T) {
	ids := []string{"111111111111", "222222222222", "333333333333"}

	got := formatMigratePartialSummary([]string{"111111111111"}, ids, 1)
	if !strings.Contains(got, "1 of 3 succeeded") {
		t.Fatalf("missing count: %q", got)
	}
	if !strings.Contains(got, "Succeeded: 111111111111") {
		t.Fatalf("missing succeeded list: %q", got)
	}
	if !strings.Contains(got, "Failed:    222222222222") {
		t.Fatalf("missing failed: %q", got)
	}
	if !strings.Contains(got, "Remaining: 333333333333") {
		t.Fatalf("missing remaining: %q", got)
	}

	firstFail := formatMigratePartialSummary(nil, ids, 0)
	if strings.Contains(firstFail, "Succeeded:") {
		t.Fatalf("should omit empty succeeded list: %q", firstFail)
	}
	if !strings.Contains(firstFail, "Failed:    111111111111") {
		t.Fatalf("missing first failure: %q", firstFail)
	}
	if !strings.Contains(firstFail, "Remaining: 222222222222, 333333333333") {
		t.Fatalf("missing remaining after first fail: %q", firstFail)
	}
}

func TestWrapMigrateTrustUpdateError(t *testing.T) {
	err := wrapMigrateTrustUpdateError(fmt.Errorf("access denied"), "111111111111", "OrganizationAccountAccessRole", "rh-control", "osd-staging-1")
	msg := err.Error()
	for _, want := range []string{
		"access denied",
		"111111111111 is already in destination payer osd-staging-1",
		"still trusts source payer rh-control",
		"Assume OrganizationAccountAccessRole from rh-control",
		"not a rollback",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in: %s", want, msg)
		}
	}
}

func TestWrapMigrateOUMoveError(t *testing.T) {
	err := wrapMigrateOUMoveError(fmt.Errorf("constraint"), "111111111111", "ou-abcd-12345678", "osd-staging-1")
	msg := err.Error()
	for _, want := range []string{
		"constraint",
		"membership and role trust are updated",
		"move to ou-abcd-12345678 failed",
		"destination payer osd-staging-1",
		"--destination-ou",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in: %s", want, msg)
		}
	}
}

func TestAWSMigrateAccountPartialFailureSummary(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetAWSAlias("osd-staging-1", "987654321098")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"111111111111", "222222222222"} {
		cfg, err = cfg.SetLinkedAccount(id, configstore.LinkedAccount{
			AccountID:  id,
			PayerAlias: "rh-control",
			Role:       "OrganizationAccountAccessRole",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := migrateAccountEnsureCredentials
	origLoad := migrateAccountLoadConfigForCreds
	origContains := migrateAccountContains
	origInvite := migrateAccountInvite
	defer func() {
		migrateAccountEnsureCredentials = origEnsure
		migrateAccountLoadConfigForCreds = origLoad
		migrateAccountContains = origContains
		migrateAccountInvite = origInvite
	}()

	migrateAccountEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	migrateAccountLoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	containsCalls := map[string]int{}
	migrateAccountContains = func(_ context.Context, _ aws.Config, accountID string) (bool, error) {
		containsCalls[accountID]++
		if accountID == "111111111111" {
			return containsCalls[accountID] == 1, nil // in source, not dest
		}
		// 222222222222: not in source → fail before invite
		return false, nil
	}
	migrateAccountInvite = func(context.Context, aws.Config, string, string) (coreaccount.InviteResult, error) {
		t.Fatal("invite should not run on dry-run")
		return coreaccount.InviteResult{}, nil
	}

	migrateAccountIDFlag = "111111111111,222222222222"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "osd-staging-1"
	migrateAccountDestOU = ""
	migrateAccountRole = ""
	migrateAccountNotes = ""
	migrateAccountDryRun = true
	migrateAccountYes = false
	awsFlags.ConfigPath = configPath
	defer func() {
		migrateAccountIDFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("role", "", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	err = runAWSMigrateAccount(cmd, nil)
	if err == nil {
		t.Fatal("expected failure on second account")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Partial run: 1 of 2 succeeded") {
		t.Fatalf("expected partial summary in error, got %v", err)
	}
	if !strings.Contains(msg, "Succeeded: 111111111111") {
		t.Fatalf("expected succeeded ID in error, got %v", err)
	}
	if !strings.Contains(msg, "Failed:    222222222222") {
		t.Fatalf("expected failed ID in error, got %v", err)
	}
	if !strings.Contains(out.String(), "Dry run complete") {
		t.Fatalf("first account should have completed dry-run: %q", out.String())
	}
}

func TestResolveMigrateAccountTargetFromID(t *testing.T) {
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetLinkedAccount("tenant-a", configstore.LinkedAccount{
		AccountID:  "111111111111",
		PayerAlias: "rh-control",
		Role:       "CustomRole",
	})
	if err != nil {
		t.Fatal(err)
	}
	migrateAccountRole = ""
	defer func() {
		migrateAccountRole = ""
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("role", "", "")
	alias, role, err := resolveMigrateAccountTarget(cmd, cfg, "", "111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if alias != "tenant-a" || role != "CustomRole" {
		t.Fatalf("got alias=%s role=%s", alias, role)
	}
}
