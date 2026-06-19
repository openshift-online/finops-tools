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

const (
	snowflakeConnectTimeout = 30 * time.Second
	initFailCooldown        = 30 * time.Second
)

// ErrUnavailable is returned when Snowflake is configured but not connected.
var ErrUnavailable = errors.New("snowflake unavailable")

// LazyService connects to Snowflake on the first query so HTTP startup is not blocked.
type LazyService struct {
	connect coresnowflake.ConnectParams
	maxRows int
	logger  *slog.Logger

	mu           sync.Mutex
	db           *sql.DB
	svc          *Service
	init         bool
	initErr      error
	lastInitFail time.Time
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

// QueryUnlimited executes SQL without a row cap.
func (l *LazyService) QueryUnlimited(ctx context.Context, sqlText string) (QueryResponse, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, snowflakeConnectTimeout)
	defer cancel()
	svc, err := l.serviceWithContext(ctxWithTimeout)
	if err != nil {
		return QueryResponse{}, err
	}
	return svc.QueryUnlimited(ctx, sqlText)
}

// Database returns the underlying Snowflake database handle.
func (l *LazyService) Database(ctx context.Context) (*sql.DB, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, snowflakeConnectTimeout)
	defer cancel()
	svc, err := l.serviceWithContext(ctxWithTimeout)
	if err != nil {
		return nil, err
	}
	return svc.DB, nil
}

// Check verifies Snowflake connectivity for readiness probes. Unlike Query,
// it retries after prior connection failures and re-validates an existing pool.
func (l *LazyService) Check(ctx context.Context) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, snowflakeConnectTimeout)
	defer cancel()

	for {
		l.mu.Lock()
		if l.svc == nil {
			if l.init && l.initErr != nil {
				l.clearInitFailureLocked()
			}
			l.mu.Unlock()
			_, err := l.serviceWithContext(ctxWithTimeout)
			return err
		}
		db := l.db
		l.mu.Unlock()

		if err := coresnowflake.Ping(ctxWithTimeout, db); err == nil {
			return nil
		}

		l.mu.Lock()
		if l.svc == nil {
			l.mu.Unlock()
			continue
		}
		if l.db != db {
			// Pool was replaced while we pinged a stale handle; re-validate the current one.
			l.mu.Unlock()
			continue
		}
		_ = l.db.Close()
		l.db = nil
		l.svc = nil
		l.init = false
		l.initErr = nil
		l.mu.Unlock()

		_, err := l.serviceWithContext(ctxWithTimeout)
		return err
	}
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
	l.lastInitFail = time.Time{}
	return err
}

func (l *LazyService) clearInitFailureLocked() {
	l.init = false
	l.initErr = nil
}

func (l *LazyService) recordInitFailureLocked(err error) {
	l.init = true
	l.initErr = err
	l.lastInitFail = time.Now()
}

func (l *LazyService) serviceWithContext(ctx context.Context) (*Service, error) {
	l.mu.Lock()
	if l.svc != nil {
		svc := l.svc
		l.mu.Unlock()
		return svc, nil
	}
	if l.init && l.initErr != nil {
		if time.Since(l.lastInitFail) < initFailCooldown {
			err := l.initErr
			l.mu.Unlock()
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		l.clearInitFailureLocked()
	}
	l.mu.Unlock()

	db, err := coresnowflake.OpenDB(l.connect)
	if err != nil {
		l.mu.Lock()
		l.recordInitFailureLocked(err)
		l.mu.Unlock()
		l.logger.Error("snowflake open failed", "error", err)
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	if err := coresnowflake.Ping(ctx, db); err != nil {
		_ = db.Close()
		l.mu.Lock()
		l.recordInitFailureLocked(err)
		l.mu.Unlock()
		l.logger.Error("snowflake ping failed", "error", err)
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.svc != nil {
		_ = db.Close()
		return l.svc, nil
	}

	l.db = db
	l.svc = &Service{DB: db, MaxRows: l.maxRows}
	l.init = true
	l.initErr = nil
	l.logger.Info("snowflake connected",
		"account", l.connect.Account,
		"warehouse", l.connect.Warehouse,
	)
	return l.svc, nil
}
