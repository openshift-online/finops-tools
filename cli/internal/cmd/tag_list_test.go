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
	"github.com/openshift-online/finops-tools/cli/internal/output"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
)

func TestRunAccountTagsLinkedAliasUsesPayerCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetLinkedAccount("osd-tenant-1", configstore.LinkedAccount{
		AccountID:  "111111111111",
		PayerAlias: "rh-control",
		Role:       "OrganizationAccountAccessRole",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := configstore.Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	awsFlags.ConfigPath = path
	awsFlags.AuthMethod = "profile"
	accountTagsFormat = string(output.FormatPrettyPrint)
	accountTagsPayer = ""
	accountTagsAlias = "osd-tenant-1"
	accountTagsAccountID = ""
	t.Cleanup(func() {
		awsFlags.ConfigPath = ""
		awsFlags.AuthMethod = ""
		accountTagsFormat = ""
		accountTagsPayer = ""
		accountTagsAlias = ""
		accountTagsAccountID = ""
	})

	origEnsure := accountTagsEnsureCredentials
	origLoadConfig := accountTagsLoadConfigForCreds
	origFetch := accountTagsFetch
	t.Cleanup(func() {
		accountTagsEnsureCredentials = origEnsure
		accountTagsLoadConfigForCreds = origLoadConfig
		accountTagsFetch = origFetch
	})

	accountTagsEnsureCredentials = func(_ context.Context, opts awsauth.EnsureOptions) (awsconfig.Result, error) {
		if opts.AccountName != "123456789012" {
			t.Fatalf("ensure AccountName = %q", opts.AccountName)
		}
		return awsconfig.Result{Profile: "rh-control"}, nil
	}
	accountTagsLoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	accountTagsFetch = func(_ context.Context, _ aws.Config, accountID string) ([]coreaccount.Tag, error) {
		if accountID != "111111111111" {
			t.Fatalf("fetch accountID = %q", accountID)
		}
		return []coreaccount.Tag{
			{Key: "env", Value: "prod"},
			{Key: "owner", Value: "team-a"},
		}, nil
	}

	buf := new(bytes.Buffer)
	tagListCmd.SetOut(buf)
	tagListCmd.SetErr(buf)
	if err := runTagList(tagListCmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"osd-tenant-1 (111111111111)",
		"AWS account tags",
		"env",
		"owner",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunAccountTagsJSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	awsFlags.ConfigPath = path
	awsFlags.AuthMethod = "profile"
	accountTagsFormat = string(output.FormatJSON)
	accountTagsPayer = ""
	accountTagsAlias = "rh-control"
	accountTagsAccountID = ""
	t.Cleanup(func() {
		awsFlags.ConfigPath = ""
		awsFlags.AuthMethod = ""
		accountTagsFormat = ""
		accountTagsPayer = ""
		accountTagsAlias = ""
		accountTagsAccountID = ""
	})

	origEnsure := accountTagsEnsureCredentials
	origLoadConfig := accountTagsLoadConfigForCreds
	origFetch := accountTagsFetch
	t.Cleanup(func() {
		accountTagsEnsureCredentials = origEnsure
		accountTagsLoadConfigForCreds = origLoadConfig
		accountTagsFetch = origFetch
	})

	accountTagsEnsureCredentials = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{Profile: "rh-control"}, nil
	}
	accountTagsLoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	accountTagsFetch = func(_ context.Context, _ aws.Config, _ string) ([]coreaccount.Tag, error) {
		return []coreaccount.Tag{
			{Key: "env", Value: "prod"},
		}, nil
	}

	buf := new(bytes.Buffer)
	tagListCmd.SetOut(buf)
	tagListCmd.SetErr(buf)
	if err := runTagList(tagListCmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"\"AccountID\": \"123456789012\"", "\"Key\": \"env\""} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunAccountTagsInvalidFormat(t *testing.T) {
	accountTagsFormat = "yaml"
	accountTagsPayer = ""
	accountTagsAlias = "rh-control"
	accountTagsAccountID = ""
	t.Cleanup(func() {
		accountTagsFormat = ""
		accountTagsPayer = ""
		accountTagsAlias = ""
		accountTagsAccountID = ""
	})
	err := runTagList(tagListCmd, nil)
	if err == nil {
		t.Fatal("expected invalid format error")
	}
	if !strings.Contains(err.Error(), "supported: pretty-print, json, csv") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAccountTagsWithPayerOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	awsFlags.ConfigPath = path
	awsFlags.AuthMethod = "profile"
	accountTagsFormat = string(output.FormatPrettyPrint)
	accountTagsPayer = "rh-control"
	accountTagsAlias = ""
	accountTagsAccountID = "111111111111"
	t.Cleanup(func() {
		awsFlags.ConfigPath = ""
		awsFlags.AuthMethod = ""
		accountTagsFormat = ""
		accountTagsPayer = ""
		accountTagsAlias = ""
		accountTagsAccountID = ""
	})

	origEnsure := accountTagsEnsureCredentials
	origLoadConfig := accountTagsLoadConfigForCreds
	origFetch := accountTagsFetch
	t.Cleanup(func() {
		accountTagsEnsureCredentials = origEnsure
		accountTagsLoadConfigForCreds = origLoadConfig
		accountTagsFetch = origFetch
	})

	accountTagsEnsureCredentials = func(_ context.Context, opts awsauth.EnsureOptions) (awsconfig.Result, error) {
		if opts.AccountName != "123456789012" {
			t.Fatalf("ensure AccountName = %q", opts.AccountName)
		}
		return awsconfig.Result{Profile: "rh-control"}, nil
	}
	accountTagsLoadConfigForCreds = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	accountTagsFetch = func(_ context.Context, _ aws.Config, accountID string) ([]coreaccount.Tag, error) {
		if accountID != "111111111111" {
			t.Fatalf("fetch accountID = %q", accountID)
		}
		return []coreaccount.Tag{{Key: "env", Value: "prod"}}, nil
	}

	buf := new(bytes.Buffer)
	tagListCmd.SetOut(buf)
	tagListCmd.SetErr(buf)
	if err := runTagList(tagListCmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "111111111111") {
		t.Fatalf("unexpected output: %s", buf.String())
	}
}

func TestResolveAccountTagsTargetExplicit(t *testing.T) {
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetLinkedAccount("osd-tenant-1", configstore.LinkedAccount{
		AccountID:  "111111111111",
		PayerAlias: "rh-control",
		Role:       "OrganizationAccountAccessRole",
	})
	if err != nil {
		t.Fatal(err)
	}

	linked, err := resolveAccountTagsTargetExplicit(cfg, "osd-tenant-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if linked.AccountID != "111111111111" || linked.CredentialsAccountID != "123456789012" {
		t.Fatalf("linked target = %+v", linked)
	}

	payer, err := resolveAccountTagsTargetExplicit(cfg, "rh-control", "")
	if err != nil {
		t.Fatal(err)
	}
	if payer.AccountID != "123456789012" || payer.CredentialsAccountID != "123456789012" {
		t.Fatalf("payer target = %+v", payer)
	}

	byID, err := resolveAccountTagsTargetExplicit(cfg, "", "111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if byID.CredentialsAccountID != "123456789012" {
		t.Fatalf("id target = %+v", byID)
	}

	if _, err := resolveAccountTagsTargetExplicit(cfg, "not-an-account", ""); err == nil {
		t.Fatal("expected invalid alias/id error")
	}
	if _, err := resolveAccountTagsTargetExplicit(cfg, "", ""); err == nil {
		t.Fatal("expected required selector error")
	}
	if _, err := resolveAccountTagsTargetExplicit(cfg, "rh-control", "123456789012"); err == nil {
		t.Fatal("expected mutually exclusive selector error")
	}
}
