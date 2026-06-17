// Package config loads HTTP server settings from environment variables.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/core/awsaccounts"
	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
)

const (
	defaultAddr                            = ":8080"
	defaultMaxRows                         = 1000
	maxRowsLimit                           = 100000
	defaultQueryTimeout                    = 60 * time.Second
	defaultAWSAccountsHistoricalTable      = "HCMFINOPSSOURCE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT"
	defaultAWSAccountsHistoricalMaxRows    = 10000
)

var snowflakeConnectionNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// SnowflakeConnections holds named Snowflake connection parameters.
type SnowflakeConnections struct {
	Default     string
	Connections map[string]coresnowflake.ConnectParams
}

// Config is the runtime configuration for the HTTP API server.
type Config struct {
	Addr                         string
	MaxRows                      int
	QueryTimeout                 time.Duration
	AWSAccountsHistoricalTable   string
	AWSAccountsHistoricalMaxRows int
	Snowflake                    *SnowflakeConnections
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	cfg := Config{
		Addr:                         envOrDefault("FINOPS_BACKEND_ADDR", defaultAddr),
		MaxRows:                      defaultMaxRows,
		QueryTimeout:                 defaultQueryTimeout,
		AWSAccountsHistoricalTable:   envOrDefault("FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_TABLE", defaultAWSAccountsHistoricalTable),
		AWSAccountsHistoricalMaxRows: defaultAWSAccountsHistoricalMaxRows,
	}
	if _, err := awsaccounts.ValidateQueryOptions(awsaccounts.QueryOptions{
		Table: cfg.AWSAccountsHistoricalTable,
	}); err != nil {
		return Config{}, fmt.Errorf("FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_TABLE: %w", err)
	}

	if v := strings.TrimSpace(os.Getenv("FINOPS_BACKEND_MAX_ROWS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxRowsLimit {
			return Config{}, fmt.Errorf("FINOPS_BACKEND_MAX_ROWS must be an integer between 1 and %d", maxRowsLimit)
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

	if v := strings.TrimSpace(os.Getenv("FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_MAX_ROWS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxRowsLimit {
			return Config{}, fmt.Errorf("FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_MAX_ROWS must be an integer between 1 and %d", maxRowsLimit)
		}
		cfg.AWSAccountsHistoricalMaxRows = n
	}

	sf, err := loadSnowflakeConnections()
	if err != nil {
		return Config{}, err
	}
	cfg.Snowflake = sf

	return cfg, nil
}

func loadSnowflakeConnections() (*SnowflakeConnections, error) {
	namesRaw := strings.TrimSpace(os.Getenv("SNOWFLAKE_CONNECTIONS"))
	if namesRaw == "" {
		return nil, nil
	}

	names, err := parseSnowflakeConnectionNames(namesRaw)
	if err != nil {
		return nil, err
	}

	connections := make(map[string]coresnowflake.ConnectParams, len(names))
	for _, name := range names {
		if _, exists := connections[name]; exists {
			return nil, fmt.Errorf("duplicate snowflake connection %q in SNOWFLAKE_CONNECTIONS", name)
		}
		prefix := "SNOWFLAKE_CONN_" + strings.ToUpper(name) + "_"
		connect, err := loadConnectionParams(name, prefix)
		if err != nil {
			return nil, err
		}
		connections[name] = connect
	}

	defaultConn := strings.ToLower(strings.TrimSpace(os.Getenv("SNOWFLAKE_DEFAULT_CONNECTION")))
	if defaultConn == "" {
		defaultConn = names[0]
	} else if _, ok := connections[defaultConn]; !ok {
		return nil, fmt.Errorf("SNOWFLAKE_DEFAULT_CONNECTION %q is not listed in SNOWFLAKE_CONNECTIONS", defaultConn)
	}

	return &SnowflakeConnections{
		Default:     defaultConn,
		Connections: connections,
	}, nil
}

func parseSnowflakeConnectionNames(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if !snowflakeConnectionNameRE.MatchString(name) {
			return nil, fmt.Errorf("invalid snowflake connection name %q: must match %s", name, snowflakeConnectionNameRE.String())
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("SNOWFLAKE_CONNECTIONS must list at least one connection name")
	}
	return names, nil
}

func loadConnectionParams(name, prefix string) (coresnowflake.ConnectParams, error) {
	account := strings.TrimSpace(os.Getenv(prefix + "ACCOUNT"))
	user := strings.TrimSpace(os.Getenv(prefix + "USER"))
	token := strings.TrimSpace(os.Getenv(prefix + "TOKEN"))
	privateKey, err := loadPrivateKey(prefix)
	if err != nil {
		return coresnowflake.ConnectParams{}, fmt.Errorf(
			"snowflake configuration for connection %q: %w",
			name,
			err,
		)
	}
	warehouse := strings.TrimSpace(os.Getenv(prefix + "WAREHOUSE"))

	missing := make([]string, 0, 4)
	if account == "" {
		missing = append(missing, prefix+"ACCOUNT")
	}
	if user == "" {
		missing = append(missing, prefix+"USER")
	}
	if warehouse == "" {
		missing = append(missing, prefix+"WAREHOUSE")
	}
	if token == "" && privateKey == "" {
		missing = append(missing, prefix+"TOKEN, "+prefix+"PRIVATE_KEY, or "+prefix+"PRIVATE_KEY_FILE")
	}
	if len(missing) > 0 {
		return coresnowflake.ConnectParams{}, fmt.Errorf(
			"incomplete snowflake configuration for connection %q: missing %s",
			name,
			strings.Join(missing, ", "),
		)
	}

	return coresnowflake.ConnectParams{
		Account:       account,
		User:          user,
		Token:         token,
		PrivateKeyPEM: privateKey,
		Role:          strings.TrimSpace(os.Getenv(prefix + "ROLE")),
		Warehouse:            warehouse,
		Database:             strings.TrimSpace(os.Getenv(prefix + "DATABASE")),
		Schema:               strings.TrimSpace(os.Getenv(prefix + "SCHEMA")),
	}, nil
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func loadPrivateKey(prefix string) (string, error) {
	if pem := normalizePEM(os.Getenv(prefix + "PRIVATE_KEY")); pem != "" {
		return pem, nil
	}
	raw := strings.TrimSpace(os.Getenv(prefix + "PRIVATE_KEY_FILE"))
	if raw == "" {
		return "", nil
	}
	if looksLikePEM(raw) {
		return normalizePEM(raw), nil
	}
	data, err := os.ReadFile(raw)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", prefix+"PRIVATE_KEY_FILE", err)
	}
	return normalizePEM(string(data)), nil
}

func looksLikePEM(v string) bool {
	return strings.Contains(v, "-----BEGIN ")
}

// normalizePEM fixes literal \n sequences sometimes stored in Kubernetes secrets.
func normalizePEM(v string) string {
	v = strings.TrimSpace(v)
	if strings.Contains(v, `\n`) {
		v = strings.ReplaceAll(v, `\n`, "\n")
	}
	return v
}
