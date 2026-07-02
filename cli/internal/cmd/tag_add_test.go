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
)

func TestRunAccountAddTagFailsWhenKeyExistsWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	awsFlags.ConfigPath = path
	awsFlags.AuthMethod = "profile"
	accountAddTagKey = "owner"
	accountAddTagValue = "team-a"
	accountAddTagForce = false
	accountAddTagPayer = ""
	accountAddTagAlias = "rh-control"
	accountAddTagAccountID = ""
	t.Cleanup(func() {
		awsFlags.ConfigPath = ""
		awsFlags.AuthMethod = ""
		accountAddTagKey = ""
		accountAddTagValue = ""
		accountAddTagForce = false
		accountAddTagPayer = ""
		accountAddTagAlias = ""
		accountAddTagAccountID = ""
	})

	origEnsure := accountAddTagEnsureCredentialsFn
	origLoad := accountAddTagLoadConfigFn
	origList := accountAddTagListTagsFn
	origSet := accountAddTagSetAccountTagFn
	origDetect := accountAddTagDetectKindFn
	t.Cleanup(func() {
		accountAddTagEnsureCredentialsFn = origEnsure
		accountAddTagLoadConfigFn = origLoad
		accountAddTagListTagsFn = origList
		accountAddTagSetAccountTagFn = origSet
		accountAddTagDetectKindFn = origDetect
	})

	accountAddTagEnsureCredentialsFn = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{Profile: "rh-control"}, nil
	}
	accountAddTagLoadConfigFn = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	accountAddTagListTagsFn = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		return []coreaccount.Tag{{Key: "owner", Value: "team-b"}}, nil
	}
	accountAddTagSetAccountTagFn = func(context.Context, aws.Config, string, string, string) error {
		t.Fatal("set tag must not be called when key already exists without --force")
		return nil
	}
	accountAddTagDetectKindFn = func(context.Context, aws.Config, string) (coreaccount.AccountKind, error) {
		return coreaccount.AccountKindPayer, nil
	}

	err := runTagAdd(tagAddCmd, nil)
	if err == nil {
		t.Fatal("expected duplicate tag error")
	}
	if !strings.Contains(err.Error(), "use --force to overwrite") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAccountAddTagForceOnLinkedAliasUsesPayerCredentials(t *testing.T) {
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
	accountAddTagKey = "owner"
	accountAddTagValue = "team-a"
	accountAddTagForce = true
	accountAddTagPayer = ""
	accountAddTagAlias = "osd-tenant-1"
	accountAddTagAccountID = ""
	t.Cleanup(func() {
		awsFlags.ConfigPath = ""
		awsFlags.AuthMethod = ""
		accountAddTagKey = ""
		accountAddTagValue = ""
		accountAddTagForce = false
		accountAddTagPayer = ""
		accountAddTagAlias = ""
		accountAddTagAccountID = ""
	})

	origEnsure := accountAddTagEnsureCredentialsFn
	origLoad := accountAddTagLoadConfigFn
	origList := accountAddTagListTagsFn
	origSet := accountAddTagSetAccountTagFn
	origDetect := accountAddTagDetectKindFn
	t.Cleanup(func() {
		accountAddTagEnsureCredentialsFn = origEnsure
		accountAddTagLoadConfigFn = origLoad
		accountAddTagListTagsFn = origList
		accountAddTagSetAccountTagFn = origSet
		accountAddTagDetectKindFn = origDetect
	})

	accountAddTagEnsureCredentialsFn = func(_ context.Context, opts awsauth.EnsureOptions) (awsconfig.Result, error) {
		if opts.AccountName != "123456789012" {
			t.Fatalf("ensure AccountName = %q", opts.AccountName)
		}
		return awsconfig.Result{Profile: "rh-control"}, nil
	}
	accountAddTagLoadConfigFn = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	accountAddTagListTagsFn = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		return []coreaccount.Tag{{Key: "owner", Value: "team-b"}}, nil
	}
	accountAddTagSetAccountTagFn = func(_ context.Context, _ aws.Config, accountID, key, value string) error {
		if accountID != "111111111111" || key != "owner" || value != "team-a" {
			t.Fatalf("set args: account=%s key=%s value=%s", accountID, key, value)
		}
		return nil
	}
	accountAddTagDetectKindFn = func(context.Context, aws.Config, string) (coreaccount.AccountKind, error) {
		return coreaccount.AccountKindPayer, nil
	}

	buf := new(bytes.Buffer)
	tagAddCmd.SetOut(buf)
	tagAddCmd.SetErr(buf)
	if err := runTagAdd(tagAddCmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "added") {
		t.Fatalf("unexpected output: %s", buf.String())
	}
}
