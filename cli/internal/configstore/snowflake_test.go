package configstore

import (
	"path/filepath"
	"testing"
)

func TestRegisterSnowflakeAccount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	acct := SnowflakeAccount{
		Account: "ORG-ACCT",
		Role:    "PUBLIC",
		SSO:     "prod",
	}
	if err := RegisterSnowflakeAccount(path, "rhprod", acct); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := cfg.SnowflakeAccountForAlias("rhprod")
	if !ok || got.Account != "ORG-ACCT" || got.Role != "PUBLIC" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	defaultAlias, ok := cfg.Default(DefaultFQNSnowflakeAccountAlias)
	if !ok || defaultAlias != "rhprod" {
		t.Fatalf("default alias = %q ok=%v, want rhprod", defaultAlias, ok)
	}
}

func TestResolveSnowflakeAccountAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	acct := SnowflakeAccount{Account: "ORG-ACCT", Role: "PUBLIC"}
	if err := RegisterSnowflakeAccount(path, "rhprod", acct); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	alias, got, err := cfg.ResolveSnowflakeAccountAlias("")
	if err != nil || alias != "rhprod" || got.Account != "ORG-ACCT" {
		t.Fatalf("default resolve: alias=%q acct=%+v err=%v", alias, got, err)
	}

	alias, got, err = cfg.ResolveSnowflakeAccountAlias("rhprod")
	if err != nil || alias != "rhprod" {
		t.Fatalf("explicit resolve: alias=%q err=%v", alias, err)
	}

	cfg, err = cfg.SetSnowflakeAlias("sandbox", SnowflakeAccount{Account: "ORG-SBX"})
	if err != nil {
		t.Fatal(err)
	}
	alias, _, err = cfg.ResolveSnowflakeAccountAlias("")
	if err != nil || alias != "rhprod" {
		t.Fatalf("with two accounts default = %q, want rhprod (first registered); err=%v", alias, err)
	}

	cfg, err = cfg.SetDefault(DefaultFQNSnowflakeAccountAlias, "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	alias, got, err = cfg.ResolveSnowflakeAccountAlias("")
	if err != nil || alias != "sandbox" || got.Account != "ORG-SBX" {
		t.Fatalf("configured default: alias=%q acct=%+v err=%v", alias, got, err)
	}
}

func TestResolveSnowflakeSession(t *testing.T) {
	cfg := File{}
	cfg, err := cfg.SetDefault(DefaultFQNSnowflakeWarehouse, "GLOBAL_WH")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetDefault(DefaultFQNSnowflakeRole, "GLOBAL_ROLE")
	if err != nil {
		t.Fatal(err)
	}

	got := cfg.ResolveSnowflakeSession(SnowflakeAccount{
		Account:   "ORG-ACCT",
		Warehouse: "ALIAS_WH",
	})
	if got.Warehouse != "ALIAS_WH" || got.Role != "GLOBAL_ROLE" {
		t.Fatalf("got %+v, want alias warehouse and global role", got)
	}

	got = cfg.ResolveSnowflakeSession(SnowflakeAccount{Account: "ORG-ACCT"})
	if got.Warehouse != "GLOBAL_WH" || got.Role != "GLOBAL_ROLE" {
		t.Fatalf("got %+v, want global defaults", got)
	}
}

func TestValidateSnowflakeAccount(t *testing.T) {
	if err := ValidateSnowflakeAccount(SnowflakeAccount{Account: "ORG-ACCT"}, "rhsandbox"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := ValidateSnowflakeAccount(SnowflakeAccount{}, "rhsandbox"); err == nil {
		t.Fatal("expected error for missing account identifier")
	}
}

func TestValidateSnowflakeWarehouse(t *testing.T) {
	if err := ValidateSnowflakeWarehouse(SnowflakeAccount{Warehouse: "WH"}, "rhsandbox"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := ValidateSnowflakeWarehouse(SnowflakeAccount{}, "rhsandbox"); err == nil {
		t.Fatal("expected error for missing warehouse")
	}
}

func TestResolveSnowflakeOAuthClientUsesDefaultPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snowflake-oauth.yaml")
	if err := SaveSnowflakeOAuthSecrets(path, SnowflakeOAuthSecrets{
		ClientID:     "from-file",
		ClientSecret: "secret",
	}); err != nil {
		t.Fatal(err)
	}

	clientID, clientSecret, err := ResolveSnowflakeOAuthClient(path)
	if err != nil {
		t.Fatal(err)
	}
	if clientID != "from-file" || clientSecret != "secret" {
		t.Fatalf("got id=%q secret=%q", clientID, clientSecret)
	}
}

func TestResolveSnowflakeOAuthClientEmptyFlagUsesDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	defaultPath, err := DefaultSnowflakeOAuthSecretsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveSnowflakeOAuthSecrets(defaultPath, SnowflakeOAuthSecrets{
		ClientID:     "default-path-id",
		ClientSecret: "s3cret",
	}); err != nil {
		t.Fatal(err)
	}

	clientID, _, err := ResolveSnowflakeOAuthClient("")
	if err != nil {
		t.Fatal(err)
	}
	if clientID != "default-path-id" {
		t.Fatalf("client_id = %q, want default-path-id", clientID)
	}
}

func TestSnowflakeOAuthSecretsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snowflake-oauth.yaml")

	secrets := SnowflakeOAuthSecrets{ClientID: "my-client", ClientSecret: "s3cret"}
	if err := SaveSnowflakeOAuthSecrets(path, secrets); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSnowflakeOAuthSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientID != secrets.ClientID || loaded.ClientSecret != secrets.ClientSecret {
		t.Fatalf("got %+v", loaded)
	}
}
