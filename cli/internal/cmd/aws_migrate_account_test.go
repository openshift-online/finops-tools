package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	origListTags := migrateAccountListTags
	origSetTags := migrateAccountSetTags
	defer func() {
		migrateAccountEnsureCredentials = origEnsure
		migrateAccountLoadConfigForCreds = origLoad
		migrateAccountContains = origContains
		migrateAccountInvite = origInvite
		migrateAccountListTags = origListTags
		migrateAccountSetTags = origSetTags
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
	migrateAccountListTags = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		return []coreaccount.Tag{
			{Key: "owner", Value: "jdoe"},
			{Key: "organization", Value: "Hybrid Platform"},
		}, nil
	}
	migrateAccountSetTags = func(context.Context, aws.Config, string, []coreaccount.Tag) error {
		t.Fatal("set tags should not run on dry-run")
		return nil
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
	if !strings.Contains(got, "preserve tags:    2 (owner, organization)") {
		t.Fatalf("missing preserve tags in plan: %q", got)
	}
	if !strings.Contains(got, "restore account tags") {
		t.Fatalf("missing restore tags step in plan: %q", got)
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
	origListTags := migrateAccountListTags
	defer func() {
		migrateAccountEnsureCredentials = origEnsure
		migrateAccountLoadConfigForCreds = origLoad
		migrateAccountContains = origContains
		migrateAccountInvite = origInvite
		migrateAccountListTags = origListTags
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
	migrateAccountListTags = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		return nil, nil
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

func TestFormatMigratePreserveTagsLine(t *testing.T) {
	if got := formatMigratePreserveTagsLine(nil); got != "preserve tags:    0" {
		t.Fatalf("empty = %q", got)
	}
	got := formatMigratePreserveTagsLine([]coreaccount.Tag{
		{Key: "owner", Value: "jdoe"},
		{Key: "env", Value: "prod"},
	})
	if got != "preserve tags:    2 (owner, env)" {
		t.Fatalf("got %q", got)
	}
}

func TestAWSMigrateAccountRestoresTags(t *testing.T) {
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
	origAssume := migrateAccountAssumeLinked
	origLoadSession := migrateAccountLoadFromSession
	origAccept := migrateAccountAccept
	origTrust := migrateAccountUpdateTrust
	origListTags := migrateAccountListTags
	origSetTags := migrateAccountSetTags
	origUpdateConfig := migrateAccountUpdateConfig
	origInvalidate := migrateAccountInvalidateOrgCache
	defer func() {
		migrateAccountEnsureCredentials = origEnsure
		migrateAccountLoadConfigForCreds = origLoad
		migrateAccountContains = origContains
		migrateAccountInvite = origInvite
		migrateAccountAssumeLinked = origAssume
		migrateAccountLoadFromSession = origLoadSession
		migrateAccountAccept = origAccept
		migrateAccountUpdateTrust = origTrust
		migrateAccountListTags = origListTags
		migrateAccountSetTags = origSetTags
		migrateAccountUpdateConfig = origUpdateConfig
		migrateAccountInvalidateOrgCache = origInvalidate
	}()

	migrateAccountEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	migrateAccountLoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	containsCalls := 0
	migrateAccountContains = func(_ context.Context, _ aws.Config, accountID string) (bool, error) {
		containsCalls++
		if accountID != "111111111111" {
			return false, nil
		}
		return containsCalls == 1, nil
	}
	listTagsCalled := false
	migrateAccountListTags = func(_ context.Context, _ aws.Config, accountID string) ([]coreaccount.Tag, error) {
		listTagsCalled = true
		if accountID != "111111111111" {
			t.Fatalf("list tags account = %s", accountID)
		}
		return []coreaccount.Tag{{Key: "owner", Value: "jdoe"}}, nil
	}
	migrateAccountInvite = func(context.Context, aws.Config, string, string) (coreaccount.InviteResult, error) {
		return coreaccount.InviteResult{HandshakeID: "h-abc12345", State: "OPEN"}, nil
	}
	migrateAccountAssumeLinked = func(context.Context, awsconfig.EnsureLinkedOptions) (awsconfig.ProfileSession, awsconfig.Identity, error) {
		return awsconfig.ProfileSession{}, awsconfig.Identity{}, nil
	}
	migrateAccountLoadFromSession = func(context.Context, awsconfig.ProfileSession) (aws.Config, error) {
		return aws.Config{}, nil
	}
	migrateAccountAccept = func(context.Context, aws.Config, string) (coreaccount.InviteResult, error) {
		return coreaccount.InviteResult{HandshakeID: "h-abc12345", State: "ACCEPTED"}, nil
	}
	migrateAccountUpdateTrust = func(context.Context, aws.Config, string, string, string) error {
		return nil
	}
	var restoredAccount string
	var restoredTags []coreaccount.Tag
	migrateAccountSetTags = func(_ context.Context, _ aws.Config, accountID string, tags []coreaccount.Tag) error {
		restoredAccount = accountID
		restoredTags = append([]coreaccount.Tag(nil), tags...)
		return nil
	}
	migrateAccountUpdateConfig = func(string, string, string, string) error { return nil }
	migrateAccountInvalidateOrgCache = func(string, string) error { return nil }

	migrateAccountIDFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "osd-staging-1"
	migrateAccountDestOU = ""
	migrateAccountRole = ""
	migrateAccountNotes = ""
	migrateAccountDryRun = false
	migrateAccountYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		migrateAccountIDFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
		migrateAccountYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("role", "", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runAWSMigrateAccount(cmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !listTagsCalled {
		t.Fatal("expected ListTags on source before migrate")
	}
	if restoredAccount != "111111111111" {
		t.Fatalf("restored account = %q", restoredAccount)
	}
	if len(restoredTags) != 1 || restoredTags[0].Key != "owner" || restoredTags[0].Value != "jdoe" {
		t.Fatalf("restored tags = %#v", restoredTags)
	}
	if !strings.Contains(out.String(), "Restored 1 Organizations account tag(s)") {
		t.Fatalf("missing restore success line: %q", out.String())
	}
	recoveryPath := filepath.Join(dir, "migrate-tag-recovery", "111111111111.json")
	if !strings.Contains(out.String(), "Saved Organizations tag recovery snapshot to "+recoveryPath) {
		t.Fatalf("missing recovery snapshot save line: %q", out.String())
	}
	if _, err := os.Stat(recoveryPath); !os.IsNotExist(err) {
		t.Fatalf("expected recovery file removed after successful restore, stat err=%v", err)
	}
}

func TestAWSMigrateAccountKeepsTagRecoveryOnRestoreFailure(t *testing.T) {
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
	origAssume := migrateAccountAssumeLinked
	origLoadSession := migrateAccountLoadFromSession
	origAccept := migrateAccountAccept
	origTrust := migrateAccountUpdateTrust
	origListTags := migrateAccountListTags
	origSetTags := migrateAccountSetTags
	origUpdateConfig := migrateAccountUpdateConfig
	origInvalidate := migrateAccountInvalidateOrgCache
	defer func() {
		migrateAccountEnsureCredentials = origEnsure
		migrateAccountLoadConfigForCreds = origLoad
		migrateAccountContains = origContains
		migrateAccountInvite = origInvite
		migrateAccountAssumeLinked = origAssume
		migrateAccountLoadFromSession = origLoadSession
		migrateAccountAccept = origAccept
		migrateAccountUpdateTrust = origTrust
		migrateAccountListTags = origListTags
		migrateAccountSetTags = origSetTags
		migrateAccountUpdateConfig = origUpdateConfig
		migrateAccountInvalidateOrgCache = origInvalidate
	}()

	migrateAccountEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	migrateAccountLoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	containsCalls := 0
	migrateAccountContains = func(_ context.Context, _ aws.Config, accountID string) (bool, error) {
		containsCalls++
		if accountID != "111111111111" {
			return false, nil
		}
		return containsCalls == 1, nil
	}
	migrateAccountListTags = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		return []coreaccount.Tag{
			{Key: "owner", Value: "jdoe"},
			{Key: "note", Value: "trailing"},
		}, nil
	}
	migrateAccountInvite = func(context.Context, aws.Config, string, string) (coreaccount.InviteResult, error) {
		return coreaccount.InviteResult{HandshakeID: "h-abc12345", State: "OPEN"}, nil
	}
	migrateAccountAssumeLinked = func(context.Context, awsconfig.EnsureLinkedOptions) (awsconfig.ProfileSession, awsconfig.Identity, error) {
		return awsconfig.ProfileSession{}, awsconfig.Identity{}, nil
	}
	migrateAccountLoadFromSession = func(context.Context, awsconfig.ProfileSession) (aws.Config, error) {
		return aws.Config{}, nil
	}
	migrateAccountAccept = func(context.Context, aws.Config, string) (coreaccount.InviteResult, error) {
		return coreaccount.InviteResult{HandshakeID: "h-abc12345", State: "ACCEPTED"}, nil
	}
	migrateAccountSetTags = func(context.Context, aws.Config, string, []coreaccount.Tag) error {
		return fmt.Errorf("tagresource denied")
	}
	migrateAccountUpdateTrust = func(context.Context, aws.Config, string, string, string) error {
		return nil
	}
	migrateAccountUpdateConfig = func(string, string, string, string) error { return nil }
	migrateAccountInvalidateOrgCache = func(string, string) error { return nil }

	migrateAccountIDFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "osd-staging-1"
	migrateAccountDestOU = ""
	migrateAccountRole = ""
	migrateAccountNotes = ""
	migrateAccountDryRun = false
	migrateAccountYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		migrateAccountIDFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
		migrateAccountYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("role", "", "")
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := runAWSMigrateAccount(cmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	recoveryPath := filepath.Join(dir, "migrate-tag-recovery", "111111111111.json")
	if !strings.Contains(out.String(), "Saved Organizations tag recovery snapshot to "+recoveryPath) {
		t.Fatalf("missing recovery snapshot save line: %q", out.String())
	}
	if !strings.Contains(errOut.String(), recoveryPath) {
		t.Fatalf("stderr missing recovery path %q: %q", recoveryPath, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--tag-value '<value>'") {
		t.Fatalf("stderr missing quoted recovery hint: %q", errOut.String())
	}
	if strings.Contains(errOut.String(), "jdoe") {
		t.Fatalf("stderr should not include tag values: %q", errOut.String())
	}

	data, err := os.ReadFile(recoveryPath)
	if err != nil {
		t.Fatalf("read recovery file: %v", err)
	}
	info, err := os.Stat(recoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	if perms := info.Mode().Perm(); perms != 0o600 {
		t.Fatalf("recovery file perms = %o, want 0600", perms)
	}
	var got migrateTagRecoveryFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode recovery: %v", err)
	}
	if got.AccountID != "111111111111" || got.Payer != "osd-staging-1" {
		t.Fatalf("recovery meta = %#v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != (coreaccount.Tag{Key: "owner", Value: "jdoe"}) ||
		got.Tags[1] != (coreaccount.Tag{Key: "note", Value: "trailing"}) {
		t.Fatalf("recovery tags = %#v", got.Tags)
	}
}

func TestWriteMigrateTagRecoveryFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	path, err := writeMigrateTagRecoveryFile(configPath, "111111111111", "osd-staging-1", []coreaccount.Tag{
		{Key: "owner", Value: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(dir, "migrate-tag-recovery", "111111111111.json")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	if err := removeMigrateTagRecoveryFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected recovery file removed, stat err=%v", err)
	}
	if err := removeMigrateTagRecoveryFile(path); err != nil {
		t.Fatalf("remove missing file should be no-op: %v", err)
	}
}

func TestAWSMigrateAccountWritesTagRecoveryBeforeInvite(t *testing.T) {
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
	origListTags := migrateAccountListTags
	origSetTags := migrateAccountSetTags
	defer func() {
		migrateAccountEnsureCredentials = origEnsure
		migrateAccountLoadConfigForCreds = origLoad
		migrateAccountContains = origContains
		migrateAccountInvite = origInvite
		migrateAccountListTags = origListTags
		migrateAccountSetTags = origSetTags
	}()

	migrateAccountEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	migrateAccountLoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	containsCalls := 0
	migrateAccountContains = func(_ context.Context, _ aws.Config, accountID string) (bool, error) {
		containsCalls++
		if accountID != "111111111111" {
			return false, nil
		}
		return containsCalls == 1, nil
	}
	migrateAccountListTags = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		return []coreaccount.Tag{{Key: "owner", Value: "jdoe"}}, nil
	}
	migrateAccountInvite = func(context.Context, aws.Config, string, string) (coreaccount.InviteResult, error) {
		recoveryPath := filepath.Join(dir, "migrate-tag-recovery", "111111111111.json")
		if _, err := os.Stat(recoveryPath); err != nil {
			t.Fatalf("expected recovery file before invite: %v", err)
		}
		return coreaccount.InviteResult{}, fmt.Errorf("invite denied")
	}
	migrateAccountSetTags = func(context.Context, aws.Config, string, []coreaccount.Tag) error {
		t.Fatal("set tags should not run when invite fails")
		return nil
	}

	migrateAccountIDFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "osd-staging-1"
	migrateAccountDestOU = ""
	migrateAccountRole = ""
	migrateAccountNotes = ""
	migrateAccountDryRun = false
	migrateAccountYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		migrateAccountIDFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
		migrateAccountYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("role", "", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	err = runAWSMigrateAccount(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "invite denied") {
		t.Fatalf("expected invite error, got %v", err)
	}
	recoveryPath := filepath.Join(dir, "migrate-tag-recovery", "111111111111.json")
	if !strings.Contains(out.String(), "Saved Organizations tag recovery snapshot to "+recoveryPath) {
		t.Fatalf("missing recovery snapshot save line: %q", out.String())
	}
	data, readErr := os.ReadFile(recoveryPath)
	if readErr != nil {
		t.Fatalf("recovery file should remain after invite failure: %v", readErr)
	}
	var got migrateTagRecoveryFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode recovery: %v", err)
	}
	if len(got.Tags) != 1 || got.Tags[0] != (coreaccount.Tag{Key: "owner", Value: "jdoe"}) {
		t.Fatalf("recovery tags = %#v", got.Tags)
	}
}

func TestAWSMigrateAccountRestoresTagsBeforeTrustFailure(t *testing.T) {
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
	origAssume := migrateAccountAssumeLinked
	origLoadSession := migrateAccountLoadFromSession
	origAccept := migrateAccountAccept
	origTrust := migrateAccountUpdateTrust
	origListTags := migrateAccountListTags
	origSetTags := migrateAccountSetTags
	origUpdateConfig := migrateAccountUpdateConfig
	origInvalidate := migrateAccountInvalidateOrgCache
	defer func() {
		migrateAccountEnsureCredentials = origEnsure
		migrateAccountLoadConfigForCreds = origLoad
		migrateAccountContains = origContains
		migrateAccountInvite = origInvite
		migrateAccountAssumeLinked = origAssume
		migrateAccountLoadFromSession = origLoadSession
		migrateAccountAccept = origAccept
		migrateAccountUpdateTrust = origTrust
		migrateAccountListTags = origListTags
		migrateAccountSetTags = origSetTags
		migrateAccountUpdateConfig = origUpdateConfig
		migrateAccountInvalidateOrgCache = origInvalidate
	}()

	migrateAccountEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	migrateAccountLoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	containsCalls := 0
	migrateAccountContains = func(_ context.Context, _ aws.Config, accountID string) (bool, error) {
		containsCalls++
		if accountID != "111111111111" {
			return false, nil
		}
		return containsCalls == 1, nil
	}
	migrateAccountListTags = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		return []coreaccount.Tag{{Key: "owner", Value: "jdoe"}}, nil
	}
	migrateAccountInvite = func(context.Context, aws.Config, string, string) (coreaccount.InviteResult, error) {
		return coreaccount.InviteResult{HandshakeID: "h-abc12345", State: "OPEN"}, nil
	}
	migrateAccountAssumeLinked = func(context.Context, awsconfig.EnsureLinkedOptions) (awsconfig.ProfileSession, awsconfig.Identity, error) {
		return awsconfig.ProfileSession{}, awsconfig.Identity{}, nil
	}
	migrateAccountLoadFromSession = func(context.Context, awsconfig.ProfileSession) (aws.Config, error) {
		return aws.Config{}, nil
	}
	migrateAccountAccept = func(context.Context, aws.Config, string) (coreaccount.InviteResult, error) {
		return coreaccount.InviteResult{HandshakeID: "h-abc12345", State: "ACCEPTED"}, nil
	}
	setTagsCalled := false
	migrateAccountSetTags = func(context.Context, aws.Config, string, []coreaccount.Tag) error {
		setTagsCalled = true
		return nil
	}
	migrateAccountUpdateTrust = func(context.Context, aws.Config, string, string, string) error {
		return fmt.Errorf("trust update failed")
	}
	migrateAccountUpdateConfig = func(string, string, string, string) error {
		t.Fatal("config update should not run after trust failure")
		return nil
	}
	migrateAccountInvalidateOrgCache = func(string, string) error { return nil }

	migrateAccountIDFlag = "111111111111"
	migrateAccountFromPayer = "rh-control"
	migrateAccountToPayer = "osd-staging-1"
	migrateAccountDestOU = ""
	migrateAccountRole = ""
	migrateAccountNotes = ""
	migrateAccountDryRun = false
	migrateAccountYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		migrateAccountIDFlag = ""
		migrateAccountFromPayer = ""
		migrateAccountToPayer = ""
		migrateAccountDryRun = false
		migrateAccountYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.Flags().String("role", "", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	err = runAWSMigrateAccount(cmd, nil)
	if err == nil {
		t.Fatal("expected trust update error")
	}
	if !setTagsCalled {
		t.Fatal("expected tag restore before trust update failure")
	}
	if !strings.Contains(out.String(), "Restored 1 Organizations account tag(s)") {
		t.Fatalf("missing restore success line: %q", out.String())
	}
	recoveryPath := filepath.Join(dir, "migrate-tag-recovery", "111111111111.json")
	if _, err := os.Stat(recoveryPath); !os.IsNotExist(err) {
		t.Fatalf("expected recovery file removed after successful restore, stat err=%v", err)
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
