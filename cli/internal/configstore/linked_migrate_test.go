package configstore

import (
	"path/filepath"
	"testing"
)

func TestUpdateLinkedAccountPayer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterAWSAccount(path, "987654321098", "osd-staging-1"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterAWSLinkedAccount(path, "111111111111", "tenant-a", "rh-control",
		"OrganizationAccountAccessRole"); err != nil {
		t.Fatal(err)
	}

	if err := UpdateLinkedAccountPayer(path, "tenant-a", "osd-staging-1", ""); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	linked, ok := loaded.LinkedAccountForAlias("tenant-a")
	if !ok {
		t.Fatal("missing linked alias")
	}
	if linked.PayerAlias != "osd-staging-1" {
		t.Fatalf("payer_alias = %q", linked.PayerAlias)
	}
	if linked.AccountID != "111111111111" {
		t.Fatalf("account_id = %q", linked.AccountID)
	}
}

func TestUpdateLinkedAccountPayerByAccountID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterAWSAccount(path, "987654321098", "osd-staging-1"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterAWSLinkedAccount(path, "111111111111", "tenant-a", "rh-control",
		"OrganizationAccountAccessRole"); err != nil {
		t.Fatal(err)
	}

	if err := UpdateLinkedAccountPayer(path, "111111111111", "osd-staging-1", "CustomRole"); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	linked, ok := loaded.LinkedAccountForAlias("tenant-a")
	if !ok {
		t.Fatal("missing linked alias")
	}
	if linked.PayerAlias != "osd-staging-1" || linked.Role != "CustomRole" {
		t.Fatalf("linked = %+v", linked)
	}
}
