package cmd

import (
	"bytes"
	"context"
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

	migrateAccountFlag = "tenant-a"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "osd-staging-1"
	migrateAccountDestOU = ""
	migrateAccountRole = ""
	migrateAccountNotes = ""
	migrateAccountDryRun = true
	migrateAccountYes = false
	awsFlags.ConfigPath = configPath
	defer func() {
		migrateAccountFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
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
}

func TestAWSMigrateAccountRequiresYes(t *testing.T) {
	migrateAccountFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "osd-staging-1"
	migrateAccountDryRun = false
	migrateAccountYes = false
	defer func() {
		migrateAccountFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
	}()

	err := awsMigrateAccountCmd.PreRunE(awsMigrateAccountCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}

func TestAWSMigrateAccountPayersMustDifferAfterTrim(t *testing.T) {
	migrateAccountFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "  rh-control  "
	migrateAccountDryRun = true
	migrateAccountYes = false
	defer func() {
		migrateAccountFlag = ""
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

	migrateAccountFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "rhc"
	migrateAccountDryRun = true
	migrateAccountYes = false
	awsFlags.ConfigPath = configPath
	defer func() {
		migrateAccountFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	err = runAWSMigrateAccount(cmd, nil)
	if err == nil {
		t.Fatal("expected same-payer-ID error")
	}
	got := err.Error()
	if !strings.Contains(got, "rh-control") || !strings.Contains(got, "rhc") || !strings.Contains(got, "123456789012") {
		t.Fatalf("error should name both aliases and account ID, got %v", err)
	}
}

func TestResolveMigrateAccountTargetFromAlias(t *testing.T) {
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
	migrateAccountFlag = "tenant-a"
	migrateAccountRole = ""
	defer func() {
		migrateAccountFlag = ""
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("role", "", "")
	accountID, alias, role, err := resolveMigrateAccountTarget(cmd, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if accountID != "111111111111" || alias != "tenant-a" || role != "CustomRole" {
		t.Fatalf("got account=%s alias=%s role=%s", accountID, alias, role)
	}
}
