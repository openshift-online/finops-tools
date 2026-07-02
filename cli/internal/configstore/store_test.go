// store_test.go tests payer account registration and alias persistence.
package configstore

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultPathUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "finops", "config.yaml")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestRegisterAWSAccount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "finops", "config.yaml")

	if err := RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := cfg.AWSAccountIDForAlias("rh-control")
	if !ok || id != "123456789012" {
		t.Fatalf("alias lookup = %q %v", id, ok)
	}
	if !cfg.HasAWSAccount("123456789012") {
		t.Fatal("account not registered")
	}
}

func TestRegisterAWSAccountWithoutAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := RegisterAWSAccount(path, "123456789012", ""); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := cfg.AWSAccountIDForAlias("123456789012")
	if !ok || id != "123456789012" {
		t.Fatalf("got %q %v", id, ok)
	}
}

func TestResolveCostTargets(t *testing.T) {
	cfg := Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetAWSAlias("osd-staging-1", "987654321098")
	if err != nil {
		t.Fatal(err)
	}

	byAlias, err := ResolveCostTargets(cfg, nil, []string{"rh-control"}, "")
	if err != nil || len(byAlias) != 1 || byAlias[0].AccountID != "123456789012" {
		t.Fatalf("by alias: %+v %v", byAlias, err)
	}

	byID, err := ResolveCostTargets(cfg, []string{"987654321098"}, nil, "")
	if err != nil || len(byID) != 1 || byID[0].AccountID != "987654321098" {
		t.Fatalf("by id: %+v %v", byID, err)
	}

	_, err = ResolveCostTargets(cfg, []string{"111111111111"}, nil, "")
	if err == nil {
		t.Fatal("expected error for unregistered account")
	}

	_, err = ResolveCostTargets(cfg, nil, []string{"unknown"}, "")
	if err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestResolveCostTargetsLinkedAlias(t *testing.T) {
	cfg := Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetLinkedAccount("osd-tenant-1", LinkedAccount{
		AccountID: "111111111111", PayerAlias: "rh-control", Role: "OrganizationAccountAccessRole",
	})
	if err != nil {
		t.Fatal(err)
	}

	targets, err := ResolveCostTargets(cfg, nil, []string{"osd-tenant-1"}, "")
	if err != nil || len(targets) != 1 {
		t.Fatalf("targets: %+v %v", targets, err)
	}
	if targets[0].AccountID != "111111111111" || targets[0].PayerAccountID != "123456789012" {
		t.Fatalf("target = %+v", targets[0])
	}
	if targets[0].DisplayAlias != "osd-tenant-1" {
		t.Errorf("DisplayAlias = %q, want osd-tenant-1", targets[0].DisplayAlias)
	}
}

func TestResolveCostTargetsAdHocLinked(t *testing.T) {
	cfg := Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rhc", "123456789012")
	if err != nil {
		t.Fatal(err)
	}

	targets, err := ResolveCostTargets(cfg, []string{"333333333333"}, nil, "rhc")
	if err != nil || len(targets) != 1 {
		t.Fatalf("targets: %+v %v", targets, err)
	}
	if targets[0].AccountID != "333333333333" || targets[0].PayerAccountID != "123456789012" {
		t.Fatalf("target = %+v", targets[0])
	}
}

func TestResolveCostTargetsKeepsPayerWithLinked(t *testing.T) {
	cfg := Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetLinkedAccount("osd-tenant-1", LinkedAccount{
		AccountID: "111111111111", PayerAlias: "rh-control", Role: "OrganizationAccountAccessRole",
	})
	if err != nil {
		t.Fatal(err)
	}

	targets, err := ResolveCostTargets(cfg, nil, []string{"rh-control", "osd-tenant-1"}, "")
	if err != nil || len(targets) != 2 {
		t.Fatalf("targets: %+v %v", targets, err)
	}
}

func TestParseAWSAccountIDs(t *testing.T) {
	ids, err := ParseAWSAccountIDs("123456789012, 987654321098")
	if err != nil || len(ids) != 2 {
		t.Fatalf("got %v %v", ids, err)
	}
	_, err = ParseAWSAccountIDs("rh-control")
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestParseAccountAliases(t *testing.T) {
	aliases, err := ParseAccountAliases("rh-control, osd-staging-1")
	if err != nil || len(aliases) != 2 {
		t.Fatalf("got %v %v", aliases, err)
	}
}

func TestParseOUIDs(t *testing.T) {
	ids, err := ParseOUIDs("ou-abcd-1234, ou-efgh-5678")
	if err != nil || len(ids) != 2 {
		t.Fatalf("got %v %v", ids, err)
	}
	_, err = ParseOUIDs("r-root")
	if err == nil {
		t.Fatal("expected validation error for root ID")
	}
}

func TestResolveOUAccountTargets(t *testing.T) {
	cfg := Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}

	targets, err := ResolveOUAccountTargets(cfg, []string{"111111111111", "222222222222"}, "rh-control")
	if err != nil || len(targets) != 2 {
		t.Fatalf("targets: %+v %v", targets, err)
	}
	if targets[0].PayerAccountID != "123456789012" || targets[1].PayerAccountID != "123456789012" {
		t.Fatalf("payer not set: %+v", targets)
	}

	_, err = ResolveOUAccountTargets(cfg, []string{"111111111111"}, "unknown")
	if err == nil {
		t.Fatal("expected error for unknown payer alias")
	}
}

func TestRemoveAccountByAlias(t *testing.T) {
	cfg := Default()
	var err error
	cfg, err = cfg.SetAWSAlias("rh-control", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.RemoveAccountByAlias("rh-control")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.AWS.AccountAliases["rh-control"]; ok {
		t.Fatal("alias should be removed")
	}
	if _, err := cfg.RemoveAccountByAlias("missing"); err == nil {
		t.Fatal("expected error for unknown alias")
	}
}
