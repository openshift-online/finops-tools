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

	openDB func(coresnowflake.ConnectParams) (*sql.DB, error)
	ping   func(context.Context, *sql.DB) error

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
		openDB:  coresnowflake.OpenDB,
		ping:    coresnowflake.Ping,
	}
}

// Query executes SQL, connecting to Snowflake first if needed.
// On auth/session expiry it reconnects once and retries the query.
func (l *LazyService) Query(ctx context.Context, sqlText string) (QueryResponse, error) {
	var resp QueryResponse
	err := l.withService(ctx, func(svc *Service) error {
		var qErr error
		resp, qErr = svc.Query(ctx, sqlText)
		return qErr
	})
	return resp, err
}

// QueryUnlimited executes SQL without a row cap.
// On auth/session expiry it reconnects once and retries the query.
func (l *LazyService) QueryUnlimited(ctx context.Context, sqlText string) (QueryResponse, error) {
	var resp QueryResponse
	err := l.withService(ctx, func(svc *Service) error {
		var qErr error
		resp, qErr = svc.QueryUnlimited(ctx, sqlText)
		return qErr
	})
	return resp, err
}

// Database returns the underlying Snowflake database handle.
func (l *LazyService) Database(ctx context.Context) (*sql.DB, error) {
	svc, err := l.connectService(ctx)
	if err != nil {
		return nil, err
	}
	return svc.DB, nil
}

// WithDatabase runs fn with a live *sql.DB. On Snowflake auth/session expiry it
// closes the pool, reconnects once, and retries fn.
func (l *LazyService) WithDatabase(ctx context.Context, fn func(*sql.DB) error) error {
	return l.withService(ctx, func(svc *Service) error {
		return fn(svc.DB)
	})
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
			_, err := l.serviceWithContext(ctxWithTimeout, ctx)
			return err
		}
		db := l.db
		l.mu.Unlock()

		if err := l.ping(ctxWithTimeout, db); err == nil {
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
		l.invalidateLocked()
		l.mu.Unlock()

		_, err := l.serviceWithContext(ctxWithTimeout, ctx)
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

func (l *LazyService) withService(ctx context.Context, op func(*Service) error) error {
	svc, err := l.connectService(ctx)
	if err != nil {
		return err
	}
	err = op(svc)
	if err == nil || !coresnowflake.IsAuthExpired(err) {
		return err
	}
	if ctx.Err() != nil {
		// Caller already canceled/timed out; reconnecting would race a dead
		// context into the shared init-failure cooldown.
		return err
	}

	l.logger.Warn("snowflake session expired; reconnecting", "error", err)
	l.invalidateIfCurrent(svc)

	svc, err = l.connectService(ctx)
	if err != nil {
		return err
	}
	return op(svc)
}

func (l *LazyService) connectService(ctx context.Context) (*Service, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, snowflakeConnectTimeout)
	defer cancel()
	return l.serviceWithContext(ctxWithTimeout, ctx)
}

// invalidateIfCurrent closes the pool only when failing still points at the
// active service, so a concurrent reconnect cannot tear down a newer pool.
func (l *LazyService) invalidateIfCurrent(failing *Service) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if failing == nil || l.svc != failing {
		return
	}
	l.invalidateLocked()
}

func (l *LazyService) invalidateLocked() {
	if l.db != nil {
		_ = l.db.Close()
	}
	l.db = nil
	l.svc = nil
	l.init = false
	l.initErr = nil
	l.lastInitFail = time.Time{}
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

// serviceWithContext opens Snowflake using ctx for the ping deadline.
// callerCtx is the original request context: init failures are cached unless
// the caller already canceled, so a service-owned connect timeout still enters
// initFailCooldown during outages.
func (l *LazyService) serviceWithContext(ctx, callerCtx context.Context) (*Service, error) {
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

	openDB := l.openDB
	if openDB == nil {
		openDB = coresnowflake.OpenDB
	}
	ping := l.ping
	if ping == nil {
		ping = coresnowflake.Ping
	}

	cacheInitFailure := callerCtx.Err() == nil

	db, err := openDB(l.connect)
	if err != nil {
		if cacheInitFailure {
			l.mu.Lock()
			l.recordInitFailureLocked(err)
			l.mu.Unlock()
		}
		l.logger.Error("snowflake open failed", "error", err)
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	if err := ping(ctx, db); err != nil {
		_ = db.Close()
		if cacheInitFailure {
			l.mu.Lock()
			l.recordInitFailureLocked(err)
			l.mu.Unlock()
		}
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
