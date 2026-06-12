package snowflake

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
)

const snowflakeConnectTimeout = 15 * time.Second

// ErrUnavailable is returned when Snowflake is configured but not connected.
var ErrUnavailable = errors.New("snowflake unavailable")

// LazyService connects to Snowflake on the first query so HTTP startup is not blocked.
type LazyService struct {
	connect coresnowflake.ConnectParams
	maxRows int
	logger  *slog.Logger

	mu    sync.Mutex
	db    *sql.DB
	svc   *Service
	init  bool
	initErr error
}

// NewLazyService returns a querier that opens Snowflake on first use.
func NewLazyService(connect coresnowflake.ConnectParams, maxRows int, logger *slog.Logger) *LazyService {
	if logger == nil {
		logger = slog.Default()
	}
	return &LazyService{
		connect: connect,
		maxRows: maxRows,
		logger:  logger,
	}
}

// Query executes SQL, connecting to Snowflake first if needed.
func (l *LazyService) Query(ctx context.Context, sqlText string) (QueryResponse, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, snowflakeConnectTimeout)
	defer cancel()
	svc, err := l.serviceWithContext(ctxWithTimeout)
	if err != nil {
		return QueryResponse{}, err
	}
	return svc.Query(ctx, sqlText)
}

// Check verifies Snowflake connectivity for readiness probes. Unlike Query,
// it retries after prior connection failures and re-validates an existing pool.
func (l *LazyService) Check(ctx context.Context) error {
	l.mu.Lock()
	if l.svc != nil {
		err := coresnowflake.Ping(ctx, l.db)
		if err == nil {
			l.mu.Unlock()
			return nil
		}
		_ = l.db.Close()
		l.db = nil
		l.svc = nil
		l.init = false
		l.initErr = nil
	} else if l.init && l.initErr != nil {
		l.init = false
		l.initErr = nil
	}
	l.mu.Unlock()

	_, err := l.serviceWithContext(ctx)
	return err
}

// Close releases the Snowflake handle when connected.
func (l *LazyService) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.db == nil {
		return nil
	}
	err := l.db.Close()
	l.db = nil
	l.svc = nil
	l.init = false
	l.initErr = nil
	return err
}

func (l *LazyService) serviceWithContext(ctx context.Context) (*Service, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.svc != nil {
		return l.svc, nil
	}
	if l.init {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, l.initErr)
	}

	db, err := coresnowflake.OpenDB(l.connect)
	if err != nil {
		l.init = true
		l.initErr = err
		l.logger.Error("snowflake open failed", "error", err)
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	if err := coresnowflake.Ping(ctx, db); err != nil {
		_ = db.Close()
		l.init = true
		l.initErr = err
		l.logger.Error("snowflake ping failed", "error", err)
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	l.db = db
	l.svc = &Service{DB: db, MaxRows: l.maxRows}
	l.init = true
	l.logger.Info("snowflake connected",
		"account", l.connect.Account,
		"warehouse", l.connect.Warehouse,
	)
	return l.svc, nil
}
