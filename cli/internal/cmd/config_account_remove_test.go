package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
)

func TestRunConfigAccountRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	awsFlags.ConfigPath = path
	t.Cleanup(func() { awsFlags.ConfigPath = "" })

	buf := new(bytes.Buffer)
	configAccountRemoveCmd.SetOut(buf)
	configAccountRemoveCmd.SetErr(buf)
	if err := runConfigAccountRemove(configAccountRemoveCmd, []string{"rh-control"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(buf.String(), "Removed account alias") {
		t.Fatalf("unexpected output: %s", buf.String())
	}

	cfg, err := configstore.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.AWS.AccountAliases["rh-control"]; ok {
		t.Fatal("alias should be removed")
	}
}

func TestRunConfigAccountRemoveUnknownAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.Save(path, configstore.Default()); err != nil {
		t.Fatal(err)
	}

	awsFlags.ConfigPath = path
	t.Cleanup(func() { awsFlags.ConfigPath = "" })

	err := runConfigAccountRemove(configAccountRemoveCmd, []string{"missing"})
	if err == nil {
		t.Fatal("expected error for unknown alias")
	}
	if !strings.Contains(err.Error(), "unknown account alias") {
		t.Fatalf("unexpected error: %v", err)
	}
}
