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
	"github.com/spf13/cobra"
)


func stubMoveToOUVerifyParentOK(t *testing.T) {
	t.Helper()
	orig := moveToOUVerifyParent
	t.Cleanup(func() { moveToOUVerifyParent = orig })
	moveToOUVerifyParent = func(context.Context, aws.Config, string) error { return nil }
}

func TestAWSMoveToOUDryRun(t *testing.T) {
	stubMoveToOUVerifyParentOK(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
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

	origEnsure := moveToOUEnsureCredentials
	origLoad := moveToOULoadConfigForCreds
	origContains := moveToOUContains
	origParent := moveToOUParentID
	origMove := moveToOUMove
	defer func() {
		moveToOUEnsureCredentials = origEnsure
		moveToOULoadConfigForCreds = origLoad
		moveToOUContains = origContains
		moveToOUParentID = origParent
		moveToOUMove = origMove
	}()

	moveToOUEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	moveToOULoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	moveToOUContains = func(_ context.Context, _ aws.Config, accountID string) (bool, error) {
		return accountID == "111111111111", nil
	}
	moveToOUParentID = func(context.Context, aws.Config, string) (string, error) {
		return "ou-abcd-source01", nil
	}
	moveToOUMove = func(context.Context, aws.Config, string, string) error {
		t.Fatal("move should not run on dry-run")
		return nil
	}

	moveToOUAccountIDFlag = "111111111111"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-dest0001"
	moveToOUDryRun = true
	moveToOUYes = false
	awsFlags.ConfigPath = configPath
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUDryRun = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runAWSMoveToOU(cmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Dry run complete") {
		t.Fatalf("output = %q", got)
	}
	if !strings.Contains(got, "tenant-a (111111111111)") {
		t.Fatalf("missing account in plan: %q", got)
	}
	if !strings.Contains(got, "ou-abcd-source01") || !strings.Contains(got, "ou-abcd-dest0001") {
		t.Fatalf("missing from/to parents: %q", got)
	}
}


func TestAWSMoveToOUDryRunDestinationMissing(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := moveToOUEnsureCredentials
	origLoad := moveToOULoadConfigForCreds
	origVerify := moveToOUVerifyParent
	origContains := moveToOUContains
	origMove := moveToOUMove
	defer func() {
		moveToOUEnsureCredentials = origEnsure
		moveToOULoadConfigForCreds = origLoad
		moveToOUVerifyParent = origVerify
		moveToOUContains = origContains
		moveToOUMove = origMove
	}()

	moveToOUEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	moveToOULoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	moveToOUVerifyParent = func(_ context.Context, _ aws.Config, parentID string) error {
		return fmt.Errorf("parent %s not found in organization", parentID)
	}
	moveToOUContains = func(context.Context, aws.Config, string) (bool, error) {
		t.Fatal("should fail before membership check when destination is missing")
		return true, nil
	}
	moveToOUMove = func(context.Context, aws.Config, string, string) error {
		t.Fatal("move should not run when destination is missing")
		return nil
	}

	moveToOUAccountIDFlag = "111111111111"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-missing1"
	moveToOUDryRun = true
	moveToOUYes = false
	awsFlags.ConfigPath = configPath
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUDryRun = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	err = runAWSMoveToOU(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected destination not found, got %v", err)
	}
}

func TestAWSMoveToOUCacheInvalidateWarning(t *testing.T) {
	stubMoveToOUVerifyParentOK(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := moveToOUEnsureCredentials
	origLoad := moveToOULoadConfigForCreds
	origContains := moveToOUContains
	origParent := moveToOUParentID
	origMove := moveToOUMove
	origInvalidate := moveToOUInvalidateOrgCache
	defer func() {
		moveToOUEnsureCredentials = origEnsure
		moveToOULoadConfigForCreds = origLoad
		moveToOUContains = origContains
		moveToOUParentID = origParent
		moveToOUMove = origMove
		moveToOUInvalidateOrgCache = origInvalidate
	}()

	moveToOUEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	moveToOULoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	moveToOUContains = func(context.Context, aws.Config, string) (bool, error) {
		return true, nil
	}
	moveToOUParentID = func(context.Context, aws.Config, string) (string, error) {
		return "ou-abcd-source01", nil
	}
	moveToOUMove = func(context.Context, aws.Config, string, string) error {
		return nil
	}
	moveToOUInvalidateOrgCache = func(_, _ string) error {
		return fmt.Errorf("cache delete failed")
	}

	moveToOUAccountIDFlag = "111111111111"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-dest0001"
	moveToOUDryRun = false
	moveToOUYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := runAWSMoveToOU(cmd, nil); err != nil {
		t.Fatalf("move should succeed even if cache invalidate fails: %v", err)
	}
	if !strings.Contains(out.String(), "Moved account to") {
		t.Fatalf("stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "warning:") || !strings.Contains(errOut.String(), "invalidate org cache") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestAWSMoveToOURequiresYes(t *testing.T) {
	moveToOUAccountIDFlag = "111111111111"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-dest0001"
	moveToOUDryRun = false
	moveToOUYes = false
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
	}()

	err := awsMoveToOUCmd.PreRunE(awsMoveToOUCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}

func TestAWSMoveToOURequiresDestination(t *testing.T) {
	moveToOUAccountIDFlag = "111111111111"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = ""
	moveToOUDryRun = true
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUDryRun = false
	}()

	err := awsMoveToOUCmd.PreRunE(awsMoveToOUCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--destination-ou") {
		t.Fatalf("expected --destination-ou error, got %v", err)
	}
}

func TestAWSMoveToOURejectsShortOUSuffix(t *testing.T) {
	moveToOUAccountIDFlag = "111111111111"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-1234" // suffix must be at least 8 chars (matches core)
	moveToOUDryRun = true
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUDryRun = false
	}()

	err := awsMoveToOUCmd.PreRunE(awsMoveToOUCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid --destination-ou") {
		t.Fatalf("expected invalid destination-ou error, got %v", err)
	}
}

func TestAWSMoveToOUExecute(t *testing.T) {
	stubMoveToOUVerifyParentOK(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := moveToOUEnsureCredentials
	origLoad := moveToOULoadConfigForCreds
	origContains := moveToOUContains
	origParent := moveToOUParentID
	origMove := moveToOUMove
	origInvalidate := moveToOUInvalidateOrgCache
	defer func() {
		moveToOUEnsureCredentials = origEnsure
		moveToOULoadConfigForCreds = origLoad
		moveToOUContains = origContains
		moveToOUParentID = origParent
		moveToOUMove = origMove
		moveToOUInvalidateOrgCache = origInvalidate
	}()

	moved := false
	invalidated := false
	moveToOUEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	moveToOULoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	moveToOUContains = func(context.Context, aws.Config, string) (bool, error) {
		return true, nil
	}
	moveToOUParentID = func(context.Context, aws.Config, string) (string, error) {
		return "ou-abcd-source01", nil
	}
	moveToOUMove = func(_ context.Context, _ aws.Config, accountID, dest string) error {
		if accountID != "111111111111" || dest != "ou-abcd-dest0001" {
			t.Fatalf("unexpected move args account=%s dest=%s", accountID, dest)
		}
		moved = true
		return nil
	}
	moveToOUInvalidateOrgCache = func(_, payerID string) error {
		if payerID != "123456789012" {
			t.Fatalf("unexpected payerID %q", payerID)
		}
		invalidated = true
		return nil
	}

	moveToOUAccountIDFlag = "111111111111"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-dest0001"
	moveToOUDryRun = false
	moveToOUYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runAWSMoveToOU(cmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !moved {
		t.Fatal("expected MoveAccountToParent call")
	}
	if !invalidated {
		t.Fatal("expected org cache invalidation")
	}
	if !strings.Contains(out.String(), "Moved account to ou-abcd-dest0001") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestAWSMoveToOUNoopWhenSameParent(t *testing.T) {
	stubMoveToOUVerifyParentOK(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := moveToOUEnsureCredentials
	origLoad := moveToOULoadConfigForCreds
	origContains := moveToOUContains
	origParent := moveToOUParentID
	origMove := moveToOUMove
	defer func() {
		moveToOUEnsureCredentials = origEnsure
		moveToOULoadConfigForCreds = origLoad
		moveToOUContains = origContains
		moveToOUParentID = origParent
		moveToOUMove = origMove
	}()

	moveToOUEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	moveToOULoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	moveToOUContains = func(context.Context, aws.Config, string) (bool, error) {
		return true, nil
	}
	moveToOUParentID = func(context.Context, aws.Config, string) (string, error) {
		return "ou-abcd-dest0001", nil
	}
	moveToOUMove = func(context.Context, aws.Config, string, string) error {
		t.Fatal("move should not run when already under destination")
		return nil
	}

	moveToOUAccountIDFlag = "111111111111"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-dest0001"
	moveToOUDryRun = false
	moveToOUYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runAWSMoveToOU(cmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to do") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestAWSMoveToOUNotInOrg(t *testing.T) {
	stubMoveToOUVerifyParentOK(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := moveToOUEnsureCredentials
	origLoad := moveToOULoadConfigForCreds
	origContains := moveToOUContains
	origMove := moveToOUMove
	defer func() {
		moveToOUEnsureCredentials = origEnsure
		moveToOULoadConfigForCreds = origLoad
		moveToOUContains = origContains
		moveToOUMove = origMove
	}()

	moveToOUEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	moveToOULoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	moveToOUContains = func(context.Context, aws.Config, string) (bool, error) {
		return false, nil
	}
	moveToOUMove = func(context.Context, aws.Config, string, string) error {
		t.Fatal("move should not run when account is not in org")
		return nil
	}

	moveToOUAccountIDFlag = "111111111111"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-dest0001"
	moveToOUDryRun = false
	moveToOUYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	err = runAWSMoveToOU(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "not an active member") {
		t.Fatalf("expected not-in-org error, got %v", err)
	}
}

func TestAWSMoveToOURefuseManagementAccount(t *testing.T) {
	stubMoveToOUVerifyParentOK(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := moveToOUEnsureCredentials
	origLoad := moveToOULoadConfigForCreds
	origContains := moveToOUContains
	origMove := moveToOUMove
	defer func() {
		moveToOUEnsureCredentials = origEnsure
		moveToOULoadConfigForCreds = origLoad
		moveToOUContains = origContains
		moveToOUMove = origMove
	}()

	moveToOUEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	moveToOULoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	moveToOUContains = func(context.Context, aws.Config, string) (bool, error) {
		t.Fatal("should refuse management account before membership check")
		return true, nil
	}
	moveToOUMove = func(context.Context, aws.Config, string, string) error {
		t.Fatal("move should not run for management account")
		return nil
	}

	moveToOUAccountIDFlag = "123456789012"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-dest0001"
	moveToOUDryRun = false
	moveToOUYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	err = runAWSMoveToOU(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "management account") {
		t.Fatalf("expected management-account error, got %v", err)
	}
}

func TestAWSMoveToOUPartialFailure(t *testing.T) {
	stubMoveToOUVerifyParentOK(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := moveToOUEnsureCredentials
	origLoad := moveToOULoadConfigForCreds
	origContains := moveToOUContains
	origParent := moveToOUParentID
	origMove := moveToOUMove
	origInvalidate := moveToOUInvalidateOrgCache
	defer func() {
		moveToOUEnsureCredentials = origEnsure
		moveToOULoadConfigForCreds = origLoad
		moveToOUContains = origContains
		moveToOUParentID = origParent
		moveToOUMove = origMove
		moveToOUInvalidateOrgCache = origInvalidate
	}()

	moveToOUEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	moveToOULoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	moveToOUContains = func(_ context.Context, _ aws.Config, accountID string) (bool, error) {
		return accountID == "111111111111" || accountID == "222222222222", nil
	}
	moveToOUParentID = func(_ context.Context, _ aws.Config, accountID string) (string, error) {
		return "ou-abcd-source01", nil
	}
	moveToOUMove = func(_ context.Context, _ aws.Config, accountID, _ string) error {
		if accountID == "222222222222" {
			return fmt.Errorf("simulated move failure")
		}
		return nil
	}
	moveToOUInvalidateOrgCache = func(_, _ string) error { return nil }

	moveToOUAccountIDFlag = "111111111111,222222222222,333333333333"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-dest0001"
	moveToOUDryRun = false
	moveToOUYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	err = runAWSMoveToOU(cmd, nil)
	if err == nil {
		t.Fatal("expected partial failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "simulated move failure") {
		t.Fatalf("missing move error: %v", err)
	}
	if !strings.Contains(msg, "Partial run") || !strings.Contains(msg, "Succeeded: 111111111111") {
		t.Fatalf("missing partial summary: %v", err)
	}
	if !strings.Contains(msg, "Failed:    222222222222") {
		t.Fatalf("missing failed account: %v", err)
	}
	if !strings.Contains(msg, "Remaining: 333333333333") {
		t.Fatalf("missing remaining account: %v", err)
	}
	if !strings.Contains(out.String(), "Moved account to ou-abcd-dest0001") {
		t.Fatalf("expected first account moved in output: %q", out.String())
	}
}

func TestAWSMoveToOUMultiAccountSummaryExcludesNoop(t *testing.T) {
	stubMoveToOUVerifyParentOK(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	origEnsure := moveToOUEnsureCredentials
	origLoad := moveToOULoadConfigForCreds
	origContains := moveToOUContains
	origParent := moveToOUParentID
	origMove := moveToOUMove
	origInvalidate := moveToOUInvalidateOrgCache
	defer func() {
		moveToOUEnsureCredentials = origEnsure
		moveToOULoadConfigForCreds = origLoad
		moveToOUContains = origContains
		moveToOUParentID = origParent
		moveToOUMove = origMove
		moveToOUInvalidateOrgCache = origInvalidate
	}()

	moveCalls := 0
	moveToOUEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{}, nil
	}
	moveToOULoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	moveToOUContains = func(context.Context, aws.Config, string) (bool, error) {
		return true, nil
	}
	moveToOUParentID = func(_ context.Context, _ aws.Config, accountID string) (string, error) {
		if accountID == "111111111111" {
			return "ou-abcd-source01", nil
		}
		return "ou-abcd-dest0001", nil // already under destination
	}
	moveToOUMove = func(_ context.Context, _ aws.Config, accountID, _ string) error {
		moveCalls++
		if accountID != "111111111111" {
			t.Fatalf("unexpected move of %s", accountID)
		}
		return nil
	}
	moveToOUInvalidateOrgCache = func(_, _ string) error { return nil }

	moveToOUAccountIDFlag = "111111111111,222222222222"
	moveToOUPayer = "rh-control"
	moveToOUDestOU = "ou-abcd-dest0001"
	moveToOUDryRun = false
	moveToOUYes = true
	awsFlags.ConfigPath = configPath
	defer func() {
		moveToOUAccountIDFlag = ""
		moveToOUPayer = ""
		moveToOUDestOU = ""
		moveToOUYes = false
		awsFlags.ConfigPath = ""
	}()

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runAWSMoveToOU(cmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if moveCalls != 1 {
		t.Fatalf("moveCalls = %d, want 1", moveCalls)
	}
	got := out.String()
	if !strings.Contains(got, "Moved 1/2 accounts: 111111111111") {
		t.Fatalf("summary should count only real moves: %q", got)
	}
	if strings.Contains(got, "Moved 2/2") {
		t.Fatalf("should not count noop as moved: %q", got)
	}
}
