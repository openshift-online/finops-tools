package snowflake

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/snowflakedb/gosnowflake/v2"
)

// ConnectParams configures a Snowflake connection.
// Use Token for OAuth, or PrivateKeyPEM (unencrypted PEM) for JWT key-pair auth.
type ConnectParams struct {
	Account       string
	User          string
	Token         string
	PrivateKeyPEM string
	Role          string
	Warehouse     string
	Database      string
	Schema        string
}

// OpenDB opens a database/sql handle using OAuth or JWT key-pair authentication.
func OpenDB(params ConnectParams) (*sql.DB, error) {
	cfg, err := configFromParams(params)
	if err != nil {
		return nil, err
	}
	dsn, err := gosnowflake.DSN(cfg)
	if err != nil {
		return nil, fmt.Errorf("build snowflake DSN: %w", err)
	}
	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, fmt.Errorf("open snowflake: %w", err)
	}
	return db, nil
}

func configFromParams(params ConnectParams) (*gosnowflake.Config, error) {
	account := strings.TrimSpace(params.Account)
	user := strings.TrimSpace(params.User)
	if account == "" {
		return nil, fmt.Errorf("snowflake account is required")
	}
	if user == "" {
		return nil, fmt.Errorf("snowflake user is required")
	}

	keepAlive := "true"
	cfg := &gosnowflake.Config{
		Account:   account,
		User:      user,
		Role:      strings.TrimSpace(params.Role),
		Warehouse: strings.TrimSpace(params.Warehouse),
		Database:  strings.TrimSpace(params.Database),
		Schema:    strings.TrimSpace(params.Schema),
		// Heartbeat so long-lived sql.DB pools (e.g. finops-backend LazyService)
		// do not idle past Snowflake's session token TTL.
		Params: map[string]*string{
			"client_session_keep_alive": &keepAlive,
		},
	}

	privateKeyPEM := strings.TrimSpace(params.PrivateKeyPEM)
	token := strings.TrimSpace(params.Token)
	if privateKeyPEM != "" && token != "" {
		return nil, fmt.Errorf("snowflake configuration must not set both private key and oauth token")
	}

	switch {
	case privateKeyPEM != "":
		key, err := ParsePrivateKey(privateKeyPEM)
		if err != nil {
			return nil, err
		}
		cfg.Authenticator = gosnowflake.AuthTypeJwt
		cfg.PrivateKey = key
	case token != "":
		cfg.Authenticator = gosnowflake.AuthTypeOAuth
		cfg.Token = token
	default:
		return nil, fmt.Errorf("snowflake oauth token or private key is required")
	}
	return cfg, nil
}

// QueryRow runs a single-row query and scans into dest.
func QueryRow(ctx context.Context, db *sql.DB, query string, dest ...any) error {
	return db.QueryRowContext(ctx, query).Scan(dest...)
}

// Ping verifies the connection is usable.
func Ping(ctx context.Context, db *sql.DB) error {
	return db.PingContext(ctx)
}
