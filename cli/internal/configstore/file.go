// Package configstore loads and saves the finops YAML config (account aliases, defaults, linked accounts).
package configstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/account"
	"gopkg.in/yaml.v3"
)

// File is the top-level finops configuration.
type File struct {
	// Defaults holds fully qualified defaults (e.g. aws.auth_method: profile).
	Defaults  map[string]string `yaml:"defaults,omitempty"`
	AWS       AWSConfig         `yaml:"aws"`
	GCP       GCPConfig         `yaml:"gcp,omitempty"`
	Snowflake SnowflakeConfig   `yaml:"snowflake,omitempty"`
}

// AWSConfig holds AWS-specific settings.
type AWSConfig struct {
	// AccountAliases maps a friendly alias to a payer account ID (scalar) or linked account metadata (mapping).
	AccountAliases map[string]AWSAccountAlias `yaml:"account_aliases,omitempty"`
	// Defaults is deprecated; use top-level Defaults with FQN keys (e.g. aws.auth_method).
	Defaults map[string]string `yaml:"defaults,omitempty"`
}

// UnmarshalYAML loads AWS config and merges legacy linked_accounts into account_aliases.
func (c *AWSConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		AccountAliases map[string]AWSAccountAlias `yaml:"account_aliases"`
		LinkedAccounts map[string]LinkedAccount     `yaml:"linked_accounts"`
		Defaults       map[string]string            `yaml:"defaults"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.Defaults = raw.Defaults
	c.AccountAliases = raw.AccountAliases
	if c.AccountAliases == nil {
		c.AccountAliases = make(map[string]AWSAccountAlias)
	}
	for alias, linked := range raw.LinkedAccounts {
		if _, exists := c.AccountAliases[alias]; exists {
			continue
		}
		c.AccountAliases[alias] = awsAccountAliasFromLinked(linked)
	}
	return nil
}

// GCPConfig holds GCP-specific settings (reserved for future use).
type GCPConfig struct {
	AccountAliases map[string]string `yaml:"account_aliases,omitempty"`
}

// Default returns an empty configuration with initialized maps.
func Default() File {
	return File{
		Defaults: make(map[string]string),
		AWS: AWSConfig{
			AccountAliases: make(map[string]AWSAccountAlias),
		},
		GCP: GCPConfig{AccountAliases: make(map[string]string)},
		Snowflake: SnowflakeConfig{
			AccountAliases: make(map[string]SnowflakeAccount),
		},
	}
}

// Load reads configuration from path. Missing files yield a default config.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return File{}, err
	}
	var cfg File
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return File{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Defaults == nil {
		cfg.Defaults = make(map[string]string)
	}
	if cfg.AWS.AccountAliases == nil {
		cfg.AWS.AccountAliases = make(map[string]AWSAccountAlias)
	}
	if cfg.AWS.Defaults == nil {
		cfg.AWS.Defaults = make(map[string]string)
	}
	if cfg.GCP.AccountAliases == nil {
		cfg.GCP.AccountAliases = make(map[string]string)
	}
	if cfg.Snowflake.AccountAliases == nil {
		cfg.Snowflake.AccountAliases = make(map[string]SnowflakeAccount)
	}
	cfg.migrateDefaults()
	cfg.migrateAccountAliases()
	return cfg, nil
}

// Ensure loads path or creates the parent directory and an empty config file.
func Ensure(path string) (File, error) {
	cfg, err := Load(path)
	if err != nil {
		return File{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return cfg, nil
	} else if !os.IsNotExist(err) {
		return File{}, err
	}
	if err := Save(path, cfg); err != nil {
		return File{}, err
	}
	return cfg, nil
}

// Save writes configuration to path atomically.
func Save(path string, cfg File) error {
	if cfg.Defaults == nil {
		cfg.Defaults = make(map[string]string)
	}
	if cfg.AWS.AccountAliases == nil {
		cfg.AWS.AccountAliases = make(map[string]AWSAccountAlias)
	}
	if cfg.GCP.AccountAliases == nil {
		cfg.GCP.AccountAliases = make(map[string]string)
	}
	if cfg.Snowflake.AccountAliases == nil {
		cfg.Snowflake.AccountAliases = make(map[string]SnowflakeAccount)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// SetAWSAlias records alias → accountID and returns the updated config.
func (f File) SetAWSAlias(alias, accountID string) (File, error) {
	alias = strings.TrimSpace(alias)
	accountID = strings.TrimSpace(accountID)
	if alias == "" {
		return File{}, fmt.Errorf("alias is required")
	}
	if err := account.ValidateAWSAccountID(accountID); err != nil {
		return File{}, err
	}
	if f.AWS.AccountAliases == nil {
		f.AWS.AccountAliases = make(map[string]AWSAccountAlias)
	}
	f.AWS.AccountAliases[alias] = awsAccountAliasFromPayer(accountID)
	return f, nil
}

// AWSAccountIDForAlias returns the account ID for alias, if configured.
func (f File) AWSAccountIDForAlias(alias string) (string, bool) {
	entry, ok := f.AWS.AccountAliases[strings.TrimSpace(alias)]
	if !ok {
		return "", false
	}
	id := entry.AccountID()
	return id, id != ""
}

// PayerAccountIDForLinkedAccountID returns the payer account ID for a registered linked account.
func (f File) PayerAccountIDForLinkedAccountID(linkedAccountID string) (string, bool) {
	linkedAccountID = strings.TrimSpace(linkedAccountID)
	for _, entry := range f.AWS.AccountAliases {
		if entry.IsLinked() && entry.AccountID() == linkedAccountID {
			return f.PayerAccountIDForAlias(entry.PayerAlias)
		}
	}
	return "", false
}

// PayerAliasForAccountID returns a payer alias for the account ID, if configured.
func (f File) PayerAliasForAccountID(accountID string) string {
	accountID = strings.TrimSpace(accountID)
	for alias, entry := range f.AWS.AccountAliases {
		if !entry.IsLinked() && entry.AccountID() == accountID {
			return alias
		}
	}
	return ""
}

// AliasForAccountID returns the configured alias for an account ID, or the account ID itself.
func (f File) AliasForAccountID(accountID string) string {
	accountID = strings.TrimSpace(accountID)
	for alias, entry := range f.AWS.AccountAliases {
		if entry.AccountID() == accountID {
			return alias
		}
	}
	return accountID
}

// HasAWSAccount reports whether accountID is registered (directly or via an alias).
func (f File) HasAWSAccount(accountID string) bool {
	accountID = strings.TrimSpace(accountID)
	for _, entry := range f.AWS.AccountAliases {
		if entry.AccountID() == accountID {
			return true
		}
	}
	return false
}

// RemoveAccountByAlias removes alias from AWS, GCP, or Snowflake account maps.
func (f File) RemoveAccountByAlias(alias string) (File, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return File{}, fmt.Errorf("alias is required")
	}
	removed := false
	if _, ok := f.AWS.AccountAliases[alias]; ok {
		delete(f.AWS.AccountAliases, alias)
		removed = true
	}
	if _, ok := f.GCP.AccountAliases[alias]; ok {
		delete(f.GCP.AccountAliases, alias)
		removed = true
	}
	if _, ok := f.Snowflake.AccountAliases[alias]; ok {
		delete(f.Snowflake.AccountAliases, alias)
		removed = true
	}
	if !removed {
		return File{}, fmt.Errorf("unknown account alias %q", alias)
	}
	return f, nil
}
