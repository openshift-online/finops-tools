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
	svc, err := l.service()
	if err != nil {
		return QueryResponse{}, err
	}
	return svc.Query(ctx, sqlText)
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

func (l *LazyService) service() (*Service, error) {
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

	pingCtx, cancel := context.WithTimeout(context.Background(), snowflakeConnectTimeout)
	defer cancel()
	if err := coresnowflake.Ping(pingCtx, db); err != nil {
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
