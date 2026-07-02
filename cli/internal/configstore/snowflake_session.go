package configstore

import (
	"fmt"
	"strings"
)

// ResolveSnowflakeSession fills empty per-alias session fields from finops config defaults.
// Connection settings come only from ~/.config/finops/config.yaml (and flags); the CLI never
// reads ~/.snowflake/connections.toml.
func (f File) ResolveSnowflakeSession(acct SnowflakeAccount) SnowflakeAccount {
	out := acct
	if strings.TrimSpace(out.Role) == "" {
		if v, ok := f.Default(DefaultFQNSnowflakeRole); ok {
			out.Role = strings.TrimSpace(v)
		}
	}
	if strings.TrimSpace(out.Warehouse) == "" {
		if v, ok := f.Default(DefaultFQNSnowflakeWarehouse); ok {
			out.Warehouse = strings.TrimSpace(v)
		}
	}
	if strings.TrimSpace(out.Database) == "" {
		if v, ok := f.Default(DefaultFQNSnowflakeDatabase); ok {
			out.Database = strings.TrimSpace(v)
		}
	}
	if strings.TrimSpace(out.Schema) == "" {
		if v, ok := f.Default(DefaultFQNSnowflakeSchema); ok {
			out.Schema = strings.TrimSpace(v)
		}
	}
	return out
}

// ValidateSnowflakeWarehouse reports when no warehouse is configured after ResolveSnowflakeSession.
func ValidateSnowflakeWarehouse(acct SnowflakeAccount, alias string) error {
	if strings.TrimSpace(acct.Warehouse) != "" {
		return nil
	}
	return fmt.Errorf(
		"snowflake alias %q has no warehouse configured; set --warehouse on finops config account add snowflake, add warehouse under snowflake.account_aliases.%s in the finops config, or set finops config default snowflake.warehouse",
		alias, alias,
	)
}
