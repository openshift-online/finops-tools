package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("SNOWFLAKE_ACCOUNT", "")
	t.Setenv("SNOWFLAKE_USER", "")
	t.Setenv("SNOWFLAKE_TOKEN", "")
	t.Setenv("SNOWFLAKE_WAREHOUSE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != defaultAddr {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, defaultAddr)
	}
	if cfg.MaxRows != defaultMaxRows {
		t.Fatalf("MaxRows = %d, want %d", cfg.MaxRows, defaultMaxRows)
	}
	if cfg.Snowflake != nil {
		t.Fatal("expected snowflake to be nil when unset")
	}
}

func TestLoadSnowflakePartialConfig(t *testing.T) {
	t.Setenv("SNOWFLAKE_ACCOUNT", "acct")
	t.Setenv("SNOWFLAKE_USER", "")
	t.Setenv("SNOWFLAKE_TOKEN", "")
	t.Setenv("SNOWFLAKE_WAREHOUSE", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for partial snowflake config")
	}
}

func TestLoadSnowflakeComplete(t *testing.T) {
	t.Setenv("SNOWFLAKE_ACCOUNT", "acct")
	t.Setenv("SNOWFLAKE_USER", "user")
	t.Setenv("SNOWFLAKE_TOKEN", "token")
	t.Setenv("SNOWFLAKE_WAREHOUSE", "wh")
	t.Setenv("SNOWFLAKE_ROLE", "role")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Snowflake == nil {
		t.Fatal("expected snowflake config")
	}
	if cfg.Snowflake.Connect.Account != "acct" || cfg.Snowflake.Connect.Role != "role" {
		t.Fatalf("unexpected connect params: %+v", cfg.Snowflake.Connect)
	}
}

func TestLoadSnowflakePrivateKey(t *testing.T) {
	t.Setenv("SNOWFLAKE_ACCOUNT", "acct")
	t.Setenv("SNOWFLAKE_USER", "svc-user")
	t.Setenv("SNOWFLAKE_TOKEN", "")
	t.Setenv("SNOWFLAKE_PRIVATE_KEY", "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----")
	t.Setenv("SNOWFLAKE_WAREHOUSE", "wh")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Snowflake.Connect.PrivateKeyPEM == "" {
		t.Fatal("expected private key PEM")
	}
	if cfg.Snowflake.Connect.Token != "" {
		t.Fatal("expected empty oauth token when using private key")
	}
}
