// Package snowflake wraps core Snowflake query execution for the HTTP API.
package snowflake

import (
	"context"
	"database/sql"

	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
)

// QueryResponse is the outcome of a limited SQL query.
type QueryResponse struct {
	Result    coresnowflake.QueryResult
	Truncated bool
}

// Service runs SQL queries against a Snowflake database handle.
type Service struct {
	DB      *sql.DB
	MaxRows int
}

// Query executes SQL with a row cap enforced during scan.
func (s *Service) Query(ctx context.Context, sqlText string) (QueryResponse, error) {
	result, truncated, err := coresnowflake.QueryLimited(ctx, s.DB, sqlText, s.MaxRows)
	if err != nil {
		return QueryResponse{}, err
	}
	return QueryResponse{Result: result, Truncated: truncated}, nil
}

// QueryUnlimited executes SQL without a row cap.
func (s *Service) QueryUnlimited(ctx context.Context, sqlText string) (QueryResponse, error) {
	result, truncated, err := coresnowflake.QueryLimited(ctx, s.DB, sqlText, 0)
	if err != nil {
		return QueryResponse{}, err
	}
	return QueryResponse{Result: result, Truncated: truncated}, nil
}

// Ping verifies the Snowflake connection is usable.
func (s *Service) Ping(ctx context.Context) error {
	return coresnowflake.Ping(ctx, s.DB)
}
