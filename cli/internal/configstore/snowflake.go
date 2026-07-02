// snowflake.go holds Snowflake account aliases in the finops config file.
package configstore

import (
	"fmt"
	"strings"
)

// SnowflakeAccount holds non-secret Snowflake connection settings for an alias.
type SnowflakeAccount struct {
	Account   string `yaml:"account"`
	Role      string `yaml:"role,omitempty"`
	Warehouse string `yaml:"warehouse,omitempty"`
	Database  string `yaml:"database,omitempty"`
	Schema    string `yaml:"schema,omitempty"`
	// SSO selects the Red Hat SSO issuer: "prod" (default) or "stage".
	SSO string `yaml:"sso,omitempty"`
}

// SnowflakeConfig holds Snowflake-specific settings.
type SnowflakeConfig struct {
	AccountAliases map[string]SnowflakeAccount `yaml:"account_aliases,omitempty"`
}

// SetSnowflakeAlias records alias → account settings and returns the updated config.
func (f File) SetSnowflakeAlias(alias string, acct SnowflakeAccount) (File, error) {
	alias = strings.TrimSpace(alias)
	acct.Account = strings.TrimSpace(acct.Account)
	if alias == "" {
		return File{}, fmt.Errorf("alias is required")
	}
	if acct.Account == "" {
		return File{}, fmt.Errorf("snowflake account identifier is required")
	}
	if f.Snowflake.AccountAliases == nil {
		f.Snowflake.AccountAliases = make(map[string]SnowflakeAccount)
	}
	f.Snowflake.AccountAliases[alias] = acct
	return f, nil
}

// SnowflakeAccountForAlias returns settings for alias, if configured.
func (f File) SnowflakeAccountForAlias(alias string) (SnowflakeAccount, bool) {
	entry, ok := f.Snowflake.AccountAliases[strings.TrimSpace(alias)]
	if !ok {
		return SnowflakeAccount{}, false
	}
	return entry, true
}

// HasSnowflakeAccount reports whether the Snowflake account identifier is registered.
func (f File) HasSnowflakeAccount(account string) bool {
	account = strings.TrimSpace(account)
	for _, entry := range f.Snowflake.AccountAliases {
		if strings.TrimSpace(entry.Account) == account {
			return true
		}
	}
	return false
}

// RegisterSnowflakeAccount ensures the config file exists and records alias → account.
// The first registered alias becomes the default snowflake.account_alias when none is set.
func RegisterSnowflakeAccount(path, alias string, acct SnowflakeAccount) error {
	cfg, err := Ensure(path)
	if err != nil {
		return err
	}
	if alias == "" {
		alias = acct.Account
	}
	cfg, err = cfg.SetSnowflakeAlias(alias, acct)
	if err != nil {
		return err
	}
	if _, ok := cfg.Default(DefaultFQNSnowflakeAccountAlias); !ok {
		cfg, err = cfg.SetDefault(DefaultFQNSnowflakeAccountAlias, alias)
		if err != nil {
			return err
		}
	}
	return Save(path, cfg)
}

// ResolveSnowflakeAccountAlias returns the account for an explicit alias flag, the configured
// default (snowflake.account_alias), or the sole registered account when only one exists.
func (f File) ResolveSnowflakeAccountAlias(explicit string) (alias string, acct SnowflakeAccount, err error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		acct, ok := f.SnowflakeAccountForAlias(explicit)
		if !ok {
			return "", SnowflakeAccount{}, fmt.Errorf("unknown snowflake account alias %q", explicit)
		}
		return explicit, acct, nil
	}
	if v, ok := f.Default(DefaultFQNSnowflakeAccountAlias); ok {
		acct, ok := f.SnowflakeAccountForAlias(v)
		if !ok {
			return "", SnowflakeAccount{}, fmt.Errorf(
				"configured default snowflake account alias %q is not registered; run finops config default set --name snowflake.account_alias --value <alias>",
				v,
			)
		}
		return v, acct, nil
	}
	accounts := f.ListSnowflakeAccounts()
	switch len(accounts) {
	case 0:
		return "", SnowflakeAccount{}, fmt.Errorf("no snowflake accounts registered; run finops config account add snowflake")
	case 1:
		alias = accounts[0].Alias
		acct, _ = f.SnowflakeAccountForAlias(alias)
		return alias, acct, nil
	default:
		return "", SnowflakeAccount{}, fmt.Errorf(
			"multiple snowflake accounts registered; pass --account-alias or set finops config default set --name snowflake.account_alias --value <alias>",
		)
	}
}
