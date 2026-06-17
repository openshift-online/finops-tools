package config

import (
	"os"
	"strings"
	"testing"
)

// fakePEM builds PEM-shaped test material without embedding a scannable key header literal.
func fakePEM(body string) string {
	begin := "-----" + "BEGIN " + "PRIVATE KEY-----"
	end := "-----" + "END " + "PRIVATE KEY-----"
	return begin + "\n" + body + "\n" + end
}

func fakePEMEscaped(body string) string {
	begin := "-----" + "BEGIN " + "PRIVATE KEY-----"
	end := "-----" + "END " + "PRIVATE KEY-----"
	return begin + `\n` + body + `\n` + end
}

func clearSnowflakeEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SNOWFLAKE_CONNECTIONS",
		"SNOWFLAKE_DEFAULT_CONNECTION",
		"SNOWFLAKE_CONN_PROD_ACCOUNT",
		"SNOWFLAKE_CONN_PROD_USER",
		"SNOWFLAKE_CONN_PROD_TOKEN",
		"SNOWFLAKE_CONN_PROD_PRIVATE_KEY",
		"SNOWFLAKE_CONN_PROD_PRIVATE_KEY_FILE",
		"SNOWFLAKE_CONN_PROD_WAREHOUSE",
		"SNOWFLAKE_CONN_PROD_ROLE",
		"SNOWFLAKE_CONN_SANDBOX_ACCOUNT",
		"SNOWFLAKE_CONN_SANDBOX_USER",
		"SNOWFLAKE_CONN_SANDBOX_TOKEN",
		"SNOWFLAKE_CONN_SANDBOX_PRIVATE_KEY",
		"SNOWFLAKE_CONN_SANDBOX_PRIVATE_KEY_FILE",
		"SNOWFLAKE_CONN_SANDBOX_WAREHOUSE",
	} {
		t.Setenv(key, "")
	}
}

func setSingleProdConnection(t *testing.T) {
	t.Helper()
	t.Setenv("SNOWFLAKE_CONNECTIONS", "prod")
	t.Setenv("SNOWFLAKE_CONN_PROD_ACCOUNT", "acct")
	t.Setenv("SNOWFLAKE_CONN_PROD_USER", "user")
	t.Setenv("SNOWFLAKE_CONN_PROD_TOKEN", "token")
	t.Setenv("SNOWFLAKE_CONN_PROD_WAREHOUSE", "wh")
}

func TestLoadDefaults(t *testing.T) {
	clearSnowflakeEnv(t)

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
	if cfg.AWSAccountsHistoricalTable != defaultAWSAccountsHistoricalTable {
		t.Fatalf("AWSAccountsHistoricalTable = %q, want %q", cfg.AWSAccountsHistoricalTable, defaultAWSAccountsHistoricalTable)
	}
	if cfg.AWSAccountsHistoricalMaxRows != defaultAWSAccountsHistoricalMaxRows {
		t.Fatalf("AWSAccountsHistoricalMaxRows = %d, want %d", cfg.AWSAccountsHistoricalMaxRows, defaultAWSAccountsHistoricalMaxRows)
	}
	if cfg.Snowflake != nil {
		t.Fatal("expected snowflake to be nil when unset")
	}
}

func TestLoadSnowflakePartialConfig(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("SNOWFLAKE_CONNECTIONS", "prod")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for partial snowflake config")
	}
}

func TestLoadSnowflakeComplete(t *testing.T) {
	clearSnowflakeEnv(t)
	setSingleProdConnection(t)
	t.Setenv("SNOWFLAKE_CONN_PROD_ROLE", "role")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Snowflake == nil {
		t.Fatal("expected snowflake config")
	}
	if cfg.Snowflake.Default != "prod" {
		t.Fatalf("Default = %q, want prod", cfg.Snowflake.Default)
	}
	connect, ok := cfg.Snowflake.Connections["prod"]
	if !ok {
		t.Fatal("expected prod connection")
	}
	if connect.Account != "acct" || connect.Role != "role" {
		t.Fatalf("unexpected connect params: %+v", connect)
	}
}

func TestLoadSnowflakePrivateKey(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("SNOWFLAKE_CONNECTIONS", "prod")
	t.Setenv("SNOWFLAKE_CONN_PROD_ACCOUNT", "acct")
	t.Setenv("SNOWFLAKE_CONN_PROD_USER", "svc-user")
	t.Setenv("SNOWFLAKE_CONN_PROD_TOKEN", "")
	t.Setenv("SNOWFLAKE_CONN_PROD_PRIVATE_KEY", fakePEM("abc"))
	t.Setenv("SNOWFLAKE_CONN_PROD_WAREHOUSE", "wh")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	connect := cfg.Snowflake.Connections["prod"]
	if connect.PrivateKeyPEM == "" {
		t.Fatal("expected private key PEM")
	}
	if connect.Token != "" {
		t.Fatal("expected empty oauth token when using private key")
	}
}

func TestLoadSnowflakeMultipleConnections(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("SNOWFLAKE_CONNECTIONS", "prod,sandbox")
	t.Setenv("SNOWFLAKE_DEFAULT_CONNECTION", "prod")
	t.Setenv("SNOWFLAKE_CONN_PROD_ACCOUNT", "prod-acct")
	t.Setenv("SNOWFLAKE_CONN_PROD_USER", "prod-user")
	t.Setenv("SNOWFLAKE_CONN_PROD_TOKEN", "prod-token")
	t.Setenv("SNOWFLAKE_CONN_PROD_WAREHOUSE", "PROD_WH")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_ACCOUNT", "sandbox-acct")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_USER", "sandbox-user")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_PRIVATE_KEY", fakePEM("sandbox"))
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_WAREHOUSE", "SANDBOX_WH")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Snowflake.Default != "prod" {
		t.Fatalf("Default = %q, want prod", cfg.Snowflake.Default)
	}
	if len(cfg.Snowflake.Connections) != 2 {
		t.Fatalf("Connections = %d, want 2", len(cfg.Snowflake.Connections))
	}
	if cfg.Snowflake.Connections["prod"].Account != "prod-acct" {
		t.Fatalf("prod account = %q", cfg.Snowflake.Connections["prod"].Account)
	}
	if cfg.Snowflake.Connections["sandbox"].PrivateKeyPEM == "" {
		t.Fatal("expected sandbox private key")
	}
}

func TestLoadSnowflakeMultipleConnectionsDefaultFirst(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("SNOWFLAKE_CONNECTIONS", "prod,sandbox")
	t.Setenv("SNOWFLAKE_CONN_PROD_ACCOUNT", "prod-acct")
	t.Setenv("SNOWFLAKE_CONN_PROD_USER", "prod-user")
	t.Setenv("SNOWFLAKE_CONN_PROD_TOKEN", "prod-token")
	t.Setenv("SNOWFLAKE_CONN_PROD_WAREHOUSE", "PROD_WH")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_ACCOUNT", "sandbox-acct")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_USER", "sandbox-user")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_TOKEN", "sandbox-token")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_WAREHOUSE", "SANDBOX_WH")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Snowflake.Default != "prod" {
		t.Fatalf("Default = %q, want prod", cfg.Snowflake.Default)
	}
}

func TestLoadSnowflakeUnknownDefaultConnection(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("SNOWFLAKE_CONNECTIONS", "prod")
	t.Setenv("SNOWFLAKE_DEFAULT_CONNECTION", "missing")
	t.Setenv("SNOWFLAKE_CONN_PROD_ACCOUNT", "prod-acct")
	t.Setenv("SNOWFLAKE_CONN_PROD_USER", "prod-user")
	t.Setenv("SNOWFLAKE_CONN_PROD_TOKEN", "prod-token")
	t.Setenv("SNOWFLAKE_CONN_PROD_WAREHOUSE", "PROD_WH")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unknown default connection")
	}
}

func TestLoadSnowflakeInvalidConnectionName(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("SNOWFLAKE_CONNECTIONS", "1prod")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid connection name")
	}
}

func TestLoadSnowflakePrivateKeyFile(t *testing.T) {
	clearSnowflakeEnv(t)
	keyPath := writeTempKeyFile(t, fakePEM("file-key"))

	t.Setenv("SNOWFLAKE_CONNECTIONS", "prod")
	t.Setenv("SNOWFLAKE_CONN_PROD_ACCOUNT", "acct")
	t.Setenv("SNOWFLAKE_CONN_PROD_USER", "svc-user")
	t.Setenv("SNOWFLAKE_CONN_PROD_PRIVATE_KEY_FILE", keyPath)
	t.Setenv("SNOWFLAKE_CONN_PROD_WAREHOUSE", "wh")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	connect := cfg.Snowflake.Connections["prod"]
	if !strings.Contains(connect.PrivateKeyPEM, "file-key") {
		t.Fatalf("unexpected private key PEM: %q", connect.PrivateKeyPEM)
	}
}

func TestLoadSnowflakePrivateKeyFileInlinePEM(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("SNOWFLAKE_CONNECTIONS", "sandbox")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_ACCOUNT", "acct")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_USER", "svc-user")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_PRIVATE_KEY_FILE", fakePEM("inline-key"))
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_WAREHOUSE", "wh")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	connect := cfg.Snowflake.Connections["sandbox"]
	if !strings.Contains(connect.PrivateKeyPEM, "inline-key") {
		t.Fatalf("unexpected private key PEM: %q", connect.PrivateKeyPEM)
	}
}

func TestLoadSnowflakeMultipleConnectionsFromSeparateEnv(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("SNOWFLAKE_CONNECTIONS", "prod,sandbox")
	t.Setenv("SNOWFLAKE_DEFAULT_CONNECTION", "prod")
	t.Setenv("SNOWFLAKE_CONN_PROD_ACCOUNT", "prod-acct")
	t.Setenv("SNOWFLAKE_CONN_PROD_USER", "prod-user")
	t.Setenv("SNOWFLAKE_CONN_PROD_TOKEN", "prod-token")
	t.Setenv("SNOWFLAKE_CONN_PROD_WAREHOUSE", "PROD_WH")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_ACCOUNT", "sandbox-acct")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_USER", "sandbox-user")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_TOKEN", "sandbox-token")
	t.Setenv("SNOWFLAKE_CONN_SANDBOX_WAREHOUSE", "SANDBOX_WH")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Snowflake.Connections) != 2 {
		t.Fatalf("Connections = %d, want 2", len(cfg.Snowflake.Connections))
	}
}

func writeTempKeyFile(t *testing.T, contents string) string {
	t.Helper()
	path := t.TempDir() + "/private_key"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoadAWSAccountsHistoricalConfig(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_TABLE", "EXAMPLE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT")
	t.Setenv("FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_MAX_ROWS", "5000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AWSAccountsHistoricalTable != "EXAMPLE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT" {
		t.Fatalf("AWSAccountsHistoricalTable = %q", cfg.AWSAccountsHistoricalTable)
	}
	if cfg.AWSAccountsHistoricalMaxRows != 5000 {
		t.Fatalf("AWSAccountsHistoricalMaxRows = %d, want 5000", cfg.AWSAccountsHistoricalMaxRows)
	}
}

func TestLoadAWSAccountsHistoricalMaxRowsInvalid(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_MAX_ROWS", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid historical max rows")
	}
}

func TestLoadMaxRowsTooLarge(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("FINOPS_BACKEND_MAX_ROWS", "100001")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for max rows above limit")
	}
}

func TestLoadAWSAccountsHistoricalMaxRowsTooLarge(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_MAX_ROWS", "100001")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for historical max rows above limit")
	}
}

func TestLoadAWSAccountsHistoricalTableInvalid(t *testing.T) {
	clearSnowflakeEnv(t)
	t.Setenv("FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_TABLE", "bad.table")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid historical table")
	}
}

func TestNormalizePEM(t *testing.T) {
	want := fakePEM("abc")
	got := normalizePEM(fakePEMEscaped("abc"))
	if got != want {
		t.Fatalf("normalizePEM = %q, want %q", got, want)
	}
}
