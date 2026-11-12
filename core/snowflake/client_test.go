package snowflake

import (
	"strings"
	"testing"

	"github.com/snowflakedb/gosnowflake/v2"
)

func TestOpenDBRejectsMixedCredentials(t *testing.T) {
	_, err := OpenDB(ConnectParams{
		Account:       "example",
		User:          "user",
		Warehouse:     "wh",
		Token:         "token",
		PrivateKeyPEM: "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
	})
	if err == nil {
		t.Fatal("expected error for mixed credentials")
	}
}

func TestOpenDBEnablesClientSessionKeepAlive(t *testing.T) {
	cfg, err := configFromParams(ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	})
	if err != nil {
		t.Fatalf("configFromParams: %v", err)
	}
	dsn, err := gosnowflake.DSN(cfg)
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	if !strings.Contains(dsn, "client_session_keep_alive=true") {
		t.Fatalf("DSN missing keep-alive param: %s", dsn)
	}
}
