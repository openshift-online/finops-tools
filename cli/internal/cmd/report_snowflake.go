// report_snowflake.go provides the Snowflake connection used by report create
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

// HCM FinOps mapping mart location used by the hcp-hierarchy and
// executive-summary reports (golang-executive-summary-spec.md section 3.1).
const (
	hcmFinOpsDatabase = "HCMFINOPS_DB"
	hcmFinOpsSchema   = "MARTS"
)

// Telesense vCPU-hour usage mart location used by the executive-summary
// report's second Snowflake connection (spec section 3.2): "TELESENSE_DB",
// "OPENSHIFT_MARTS". Lives in a separate Snowflake account from
// hcmFinOpsDatabase/hcmFinOpsSchema -- never joined via SQL, only in
// application memory (spec sections 2 and 11.1). The opener wrapper that
// calls openSnowflakeQuerierFor with these values is added alongside the
// executive-summary generator that consumes it.

// sqlQuerier adapts *sql.DB to satisfy hcphierarchy.SnowflakeQueryer.
// *sql.Rows satisfies hcphierarchy.SnowflakeRows (Next/Scan/Close/Err).
type sqlQuerier struct {
	db *sql.DB
}

func (q *sqlQuerier) QueryContext(ctx context.Context, query string, args ...any) (hcphierarchy.SnowflakeRows, error) {
	return q.db.QueryContext(ctx, query, args...)
}

func (q *sqlQuerier) Close() error {
	return q.db.Close()
}

// openSnowflakeQuerier returns a SnowflakeQueryer for the HCP hierarchy
// report, connected to HCMFINOPS_DB.MARTS. Preserved as a thin wrapper
// around openSnowflakeQuerierFor for backward compatibility with existing
// callers/tests.
func openSnowflakeQuerier(ctx context.Context, cfgPath, snowflakeAlias string) (hcphierarchy.SnowflakeQueryer, error) {
	return openSnowflakeQuerierFor(ctx, cfgPath, snowflakeAlias, hcmFinOpsDatabase, hcmFinOpsSchema)
}

// openSnowflakeQuerierFor returns a SnowflakeQueryer for the given Snowflake
// account alias, connected to db.schema. It loads the finops config,
// resolves the alias (explicit, else the configured default, else the sole
// registered account), ensures a valid OAuth token (refreshing or
// re-authenticating as needed), and opens the connection. openSnowflakeQuerier
// delegates to this for the hcp-hierarchy/mapping connection; a Telesense
// opener wrapper calling this with TELESENSE_DB/OPENSHIFT_MARTS is added
// alongside the executive-summary generator that consumes it -- never
// attempt a cross-database SQL join between the two (spec sections 2, 11.1).
func openSnowflakeQuerierFor(ctx context.Context, cfgPath, snowflakeAlias, db, schema string) (hcphierarchy.SnowflakeQueryer, error) {
	cfg, err := configstore.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	return openSnowflakeMartQuerier(ctx, cfg, snowflakeAlias)
}

// openSnowflakeMartQuerier opens a Dataverse OAuth connection to HCMFINOPS_DB.MARTS
// for the resolved Snowflake account alias.
func openSnowflakeMartQuerier(ctx context.Context, cfg configstore.File, snowflakeAlias string) (hcphierarchy.SnowflakeQueryer, error) {
	alias, acct, err := cfg.ResolveSnowflakeAccountAlias(snowflakeAlias)
	if err != nil {
		return nil, err
	}
	if err := configstore.ValidateSnowflakeAccount(acct, alias); err != nil {
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
	conn, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
		Account:   acct.Account,
		User:      claims.SnowflakeLoginName(),
		Token:     tok.AccessToken,
		Role:      acct.Role,
		Warehouse: acct.Warehouse,
		Database:  db,
		Schema:    schema,
	})
	if err != nil {
		return nil, err
	}
	return &sqlQuerier{db: conn}, nil
}
