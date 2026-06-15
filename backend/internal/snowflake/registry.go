package snowflake

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
)

// ErrUnknownConnection is returned when a requested connection name is not configured.
var ErrUnknownConnection = errors.New("unknown snowflake connection")

// Registry routes queries to named lazy Snowflake connections.
type Registry struct {
	defaultConn string
	services    map[string]*LazyService
}

// NewRegistry returns a querier that opens each configured connection on first use.
func NewRegistry(
	defaultConn string,
	connections map[string]coresnowflake.ConnectParams,
	maxRows int,
	logger *slog.Logger,
) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	services := make(map[string]*LazyService, len(connections))
	for name, params := range connections {
		services[name] = NewLazyService(params, maxRows, logger.With("connection", name))
	}
	return &Registry{
		defaultConn: defaultConn,
		services:    services,
	}
}

// Query executes SQL on the named connection, or the default when name is empty.
func (r *Registry) Query(ctx context.Context, connection, sqlText string) (QueryResponse, error) {
	svc, err := r.serviceFor(connection)
	if err != nil {
		return QueryResponse{}, err
	}
	return svc.Query(ctx, sqlText)
}

// QueryUnlimited executes SQL without a row cap on the named connection.
func (r *Registry) QueryUnlimited(ctx context.Context, connection, sqlText string) (QueryResponse, error) {
	svc, err := r.serviceFor(connection)
	if err != nil {
		return QueryResponse{}, err
	}
	return svc.QueryUnlimited(ctx, sqlText)
}

// Database returns the Snowflake database handle for the named connection.
func (r *Registry) Database(ctx context.Context, connection string) (*sql.DB, error) {
	svc, err := r.serviceFor(connection)
	if err != nil {
		return nil, err
	}
	return svc.Database(ctx)
}

// Check verifies connectivity for the default connection (readiness probes).
func (r *Registry) Check(ctx context.Context) error {
	svc, err := r.serviceFor("")
	if err != nil {
		return err
	}
	return svc.Check(ctx)
}

// Close releases all Snowflake handles.
func (r *Registry) Close() error {
	var errs []error
	for name, svc := range r.services {
		if err := svc.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close connection %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func (r *Registry) serviceFor(connection string) (*LazyService, error) {
	name := strings.ToLower(strings.TrimSpace(connection))
	if name == "" {
		name = r.defaultConn
	}
	svc, ok := r.services[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownConnection, name)
	}
	return svc, nil
}
