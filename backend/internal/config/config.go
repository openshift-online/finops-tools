// Package config loads HTTP server settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
)

const (
	defaultAddr          = ":8080"
	defaultMaxRows       = 1000
	defaultQueryTimeout  = 60 * time.Second
)

// Snowflake holds connection parameters when Snowflake is configured.
type Snowflake struct {
	Connect coresnowflake.ConnectParams
}

// Config is the runtime configuration for the HTTP API server.
type Config struct {
	Addr          string
	MaxRows       int
	QueryTimeout  time.Duration
	Snowflake     *Snowflake
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	cfg := Config{
		Addr:         envOrDefault("FINOPS_BACKEND_ADDR", defaultAddr),
		MaxRows:      defaultMaxRows,
		QueryTimeout: defaultQueryTimeout,
	}

	if v := strings.TrimSpace(os.Getenv("FINOPS_BACKEND_MAX_ROWS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("FINOPS_BACKEND_MAX_ROWS must be a positive integer")
		}
		cfg.MaxRows = n
	}

	if v := strings.TrimSpace(os.Getenv("FINOPS_BACKEND_QUERY_TIMEOUT")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("FINOPS_BACKEND_QUERY_TIMEOUT must be a positive duration (e.g. 60s)")
		}
		cfg.QueryTimeout = d
	}

	sf, err := loadSnowflake()
	if err != nil {
		return Config{}, err
	}
	cfg.Snowflake = sf

	return cfg, nil
}

func loadSnowflake() (*Snowflake, error) {
	account := strings.TrimSpace(os.Getenv("SNOWFLAKE_ACCOUNT"))
	user := strings.TrimSpace(os.Getenv("SNOWFLAKE_USER"))
	token := strings.TrimSpace(os.Getenv("SNOWFLAKE_TOKEN"))
	privateKey := normalizePEM(os.Getenv("SNOWFLAKE_PRIVATE_KEY"))
	warehouse := strings.TrimSpace(os.Getenv("SNOWFLAKE_WAREHOUSE"))

	if account == "" && user == "" && token == "" && privateKey == "" && warehouse == "" {
		return nil, nil
	}

	missing := make([]string, 0, 4)
	if account == "" {
		missing = append(missing, "SNOWFLAKE_ACCOUNT")
	}
	if user == "" {
		missing = append(missing, "SNOWFLAKE_USER")
	}
	if warehouse == "" {
		missing = append(missing, "SNOWFLAKE_WAREHOUSE")
	}
	if token == "" && privateKey == "" {
		missing = append(missing, "SNOWFLAKE_TOKEN or SNOWFLAKE_PRIVATE_KEY")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("incomplete snowflake configuration: missing %s", strings.Join(missing, ", "))
	}

	return &Snowflake{
		Connect: coresnowflake.ConnectParams{
			Account:              account,
			User:                 user,
			Token:                token,
			PrivateKeyPEM:        privateKey,
			PrivateKeyPassphrase: strings.TrimSpace(os.Getenv("SNOWFLAKE_PRIVATE_KEY_PASSPHRASE")),
			Role:                 strings.TrimSpace(os.Getenv("SNOWFLAKE_ROLE")),
			Warehouse:            warehouse,
			Database:             strings.TrimSpace(os.Getenv("SNOWFLAKE_DATABASE")),
			Schema:               strings.TrimSpace(os.Getenv("SNOWFLAKE_SCHEMA")),
		},
	}, nil
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// normalizePEM fixes literal \n sequences sometimes stored in Kubernetes secrets.
func normalizePEM(v string) string {
	v = strings.TrimSpace(v)
	if strings.Contains(v, `\n`) {
		v = strings.ReplaceAll(v, `\n`, "\n")
	}
	return v
}
