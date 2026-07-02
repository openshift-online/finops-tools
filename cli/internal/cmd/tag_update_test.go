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

func TestRunAccountUpdateTagFailsWhenMissingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	awsFlags.ConfigPath = path
	awsFlags.AuthMethod = "profile"
	accountUpdateTagKey = "owner"
	accountUpdateTagValue = "team-a"
	accountUpdateTagForce = false
	accountUpdateTagPayer = ""
	accountUpdateTagAlias = "rh-control"
	accountUpdateTagAccountID = ""
	t.Cleanup(func() {
		awsFlags.ConfigPath = ""
		awsFlags.AuthMethod = ""
		accountUpdateTagKey = ""
		accountUpdateTagValue = ""
		accountUpdateTagForce = false
		accountUpdateTagPayer = ""
		accountUpdateTagAlias = ""
		accountUpdateTagAccountID = ""
	})

	origEnsure := accountUpdateTagEnsureCredentialsFn
	origLoad := accountUpdateTagLoadConfigFn
	origList := accountUpdateTagListTagsFn
	origSet := accountUpdateTagSetAccountTagFn
	origDetect := accountUpdateTagDetectKindFn
	t.Cleanup(func() {
		accountUpdateTagEnsureCredentialsFn = origEnsure
		accountUpdateTagLoadConfigFn = origLoad
		accountUpdateTagListTagsFn = origList
		accountUpdateTagSetAccountTagFn = origSet
		accountUpdateTagDetectKindFn = origDetect
	})

	accountUpdateTagEnsureCredentialsFn = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{Profile: "rh-control"}, nil
	}
	accountUpdateTagLoadConfigFn = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	accountUpdateTagListTagsFn = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		return []coreaccount.Tag{{Key: "env", Value: "prod"}}, nil
	}
	accountUpdateTagSetAccountTagFn = func(context.Context, aws.Config, string, string, string) error {
		t.Fatal("set tag must not be called when tag is missing without --force")
		return nil
	}
	accountUpdateTagDetectKindFn = func(context.Context, aws.Config, string) (coreaccount.AccountKind, error) {
		return coreaccount.AccountKindPayer, nil
	}

	err := runTagUpdate(tagUpdateCmd, nil)
	if err == nil {
		t.Fatal("expected missing tag error")
	}
	if !strings.Contains(err.Error(), "use --force to create it") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAccountUpdateTagForceCreatesMissingTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	awsFlags.ConfigPath = path
	awsFlags.AuthMethod = "profile"
	accountUpdateTagKey = "owner"
	accountUpdateTagValue = "team-a"
	accountUpdateTagForce = true
	accountUpdateTagPayer = ""
	accountUpdateTagAlias = "rh-control"
	accountUpdateTagAccountID = ""
	t.Cleanup(func() {
		awsFlags.ConfigPath = ""
		awsFlags.AuthMethod = ""
		accountUpdateTagKey = ""
		accountUpdateTagValue = ""
		accountUpdateTagForce = false
		accountUpdateTagPayer = ""
		accountUpdateTagAlias = ""
		accountUpdateTagAccountID = ""
	})

	origEnsure := accountUpdateTagEnsureCredentialsFn
	origLoad := accountUpdateTagLoadConfigFn
	origList := accountUpdateTagListTagsFn
	origSet := accountUpdateTagSetAccountTagFn
	origDetect := accountUpdateTagDetectKindFn
	t.Cleanup(func() {
		accountUpdateTagEnsureCredentialsFn = origEnsure
		accountUpdateTagLoadConfigFn = origLoad
		accountUpdateTagListTagsFn = origList
		accountUpdateTagSetAccountTagFn = origSet
		accountUpdateTagDetectKindFn = origDetect
	})

	accountUpdateTagEnsureCredentialsFn = func(context.Context, awsauth.EnsureOptions) (awsconfig.Result, error) {
		return awsconfig.Result{Profile: "rh-control"}, nil
	}
	accountUpdateTagLoadConfigFn = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}
	accountUpdateTagListTagsFn = func(context.Context, aws.Config, string) ([]coreaccount.Tag, error) {
		return []coreaccount.Tag{{Key: "env", Value: "prod"}}, nil
	}
	accountUpdateTagSetAccountTagFn = func(_ context.Context, _ aws.Config, accountID, key, value string) error {
		if accountID != "123456789012" || key != "owner" || value != "team-a" {
			t.Fatalf("set args: account=%s key=%s value=%s", accountID, key, value)
		}
		return nil
	}
	accountUpdateTagDetectKindFn = func(context.Context, aws.Config, string) (coreaccount.AccountKind, error) {
		return coreaccount.AccountKindPayer, nil
	}

	buf := new(bytes.Buffer)
	tagUpdateCmd.SetOut(buf)
	tagUpdateCmd.SetErr(buf)
	if err := runTagUpdate(tagUpdateCmd, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "created") {
		t.Fatalf("unexpected output: %s", buf.String())
	}
}
