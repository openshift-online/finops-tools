// report_snowflake.go provides the Snowflake connection used by report generate
// commands that query Snowflake (currently: hcp-hierarchy).
package cmd

import (
	"context"
	"database/sql"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	reportpkg "github.com/openshift-online/finops-tools/cli/internal/report"
	"github.com/openshift-online/finops-tools/cli/internal/snowflakeoauth"
	"github.com/openshift-online/finops-tools/core/hcphierarchy"
	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
)

func init() {
	reportpkg.SetSnowflakeMartOpener(openSnowflakeQuerier)
}

// sqlQuerier adapts *sql.DB to satisfy hcphierarchy.SnowflakeQueryer.
// *sql.Rows satisfies hcphierarchy.SnowflakeRows (Next/Scan/Close/Err).
type sqlQuerier struct {
	db *sql.DB
}

func (q *sqlQuerier) QueryContext(ctx context.Context, query string, args ...any) (hcphierarchy.SnowflakeRows, error) {
	return q.db.QueryContext(ctx, query, args...)
}

// openSnowflakeQuerier returns a SnowflakeQueryer for the HCP hierarchy report.
// It loads the finops config, resolves the default Snowflake account, ensures a
// valid OAuth token (refreshing or re-authenticating as needed), and opens a
// connection to HCMFINOPS_DB.MARTS.
func openSnowflakeQuerier(ctx context.Context, cfgPath, snowflakeAlias string) (hcphierarchy.SnowflakeQueryer, error) {
	cfg, err := configstore.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	alias, acct, err := cfg.ResolveSnowflakeAccountAlias(snowflakeAlias)
	if err != nil {
		return nil, err
	}
	acct = cfg.ResolveSnowflakeSession(acct)
	if err := configstore.ValidateSnowflakeWarehouse(acct, alias); err != nil {
		return nil, err
	}
	tok, oauthCfg, err := ensureSnowflakeAccessToken(ctx, cfg, alias, "", "", acct)
	if err != nil {
		return nil, err
	}
	claims, err := snowflakeoauth.ValidateDataverseToken(tok.AccessToken, oauthCfg.Audience)
	if err != nil {
		return nil, err
	}
	db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
		Account:   acct.Account,
		User:      claims.SnowflakeLoginName(),
		Token:     tok.AccessToken,
		Role:      acct.Role,
		Warehouse: acct.Warehouse,
		Database:  "HCMFINOPS_DB",
		Schema:    "MARTS",
	})
	if err != nil {
		return nil, err
	}
	return &sqlQuerier{db: db}, nil
}
