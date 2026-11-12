package snowflake

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
	"github.com/snowflakedb/gosnowflake/v2"
)

func TestLazyServiceCheckConcurrentWithClose(t *testing.T) {
	t.Parallel()

	db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = l.Check(context.Background())
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				l.mu.Lock()
				l.svc = &Service{DB: db, MaxRows: 100}
				l.db = db
				l.mu.Unlock()
				_ = l.Close()
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestLazyServiceReconnectsOnAuthExpired(t *testing.T) {
	t.Parallel()

	opens := 0
	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())
	l.openDB = func(coresnowflake.ConnectParams) (*sql.DB, error) {
		opens++
		db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
			Account:   "example",
			User:      "user",
			Warehouse: "wh",
			Token:     "token",
		})
		if err != nil {
			return nil, err
		}
		return db, nil
	}
	l.ping = func(context.Context, *sql.DB) error { return nil }

	attempts := 0
	err := l.withService(context.Background(), func(*Service) error {
		attempts++
		if attempts == 1 {
			return &gosnowflake.SnowflakeError{
				Number:  390114,
				Message: "Authentication token has expired. The user must authenticate again.",
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withService: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if opens != 2 {
		t.Fatalf("opens = %d, want 2", opens)
	}
}

func TestLazyServiceDoesNotRetryUnrelatedErrors(t *testing.T) {
	t.Parallel()

	opens := 0
	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())
	l.openDB = func(coresnowflake.ConnectParams) (*sql.DB, error) {
		opens++
		db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
			Account:   "example",
			User:      "user",
			Warehouse: "wh",
			Token:     "token",
		})
		if err != nil {
			return nil, err
		}
		return db, nil
	}
	l.ping = func(context.Context, *sql.DB) error { return nil }

	want := errors.New("SQL compilation error")
	err := l.withService(context.Background(), func(*Service) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if opens != 1 {
		t.Fatalf("opens = %d, want 1", opens)
	}
}

func TestLazyServiceWithDatabaseReconnectsOnAuthExpired(t *testing.T) {
	t.Parallel()

	opens := 0
	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())
	l.openDB = func(coresnowflake.ConnectParams) (*sql.DB, error) {
		opens++
		db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
			Account:   "example",
			User:      "user",
			Warehouse: "wh",
			Token:     "token",
		})
		if err != nil {
			return nil, err
		}
		return db, nil
	}
	l.ping = func(context.Context, *sql.DB) error { return nil }

	attempts := 0
	err := l.WithDatabase(context.Background(), func(*sql.DB) error {
		attempts++
		if attempts == 1 {
			return errors.New("390114: Authentication token has expired. The user must authenticate again.")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithDatabase: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if opens != 2 {
		t.Fatalf("opens = %d, want 2", opens)
	}
}

func TestLazyServiceSkipsReconnectWhenCallerCanceled(t *testing.T) {
	t.Parallel()

	opens := 0
	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())
	l.openDB = func(coresnowflake.ConnectParams) (*sql.DB, error) {
		opens++
		db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
			Account:   "example",
			User:      "user",
			Warehouse: "wh",
			Token:     "token",
		})
		if err != nil {
			return nil, err
		}
		return db, nil
	}
	l.ping = func(context.Context, *sql.DB) error { return nil }

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := l.withService(ctx, func(*Service) error {
		attempts++
		cancel()
		return &gosnowflake.SnowflakeError{
			Number:  390114,
			Message: "Authentication token has expired. The user must authenticate again.",
		}
	})
	if err == nil {
		t.Fatal("expected auth-expired error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if opens != 1 {
		t.Fatalf("opens = %d, want 1 (no reconnect)", opens)
	}
	if l.svc == nil {
		t.Fatal("expected existing pool to remain when reconnect is skipped")
	}
}

func TestLazyServiceInvalidateIfCurrentPreservesNewerPool(t *testing.T) {
	t.Parallel()

	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())

	oldDB, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
		Account: "example", User: "user", Warehouse: "wh", Token: "token",
	})
	if err != nil {
		t.Fatalf("OpenDB old: %v", err)
	}
	newDB, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
		Account: "example", User: "user", Warehouse: "wh", Token: "token",
	})
	if err != nil {
		t.Fatalf("OpenDB new: %v", err)
	}
	t.Cleanup(func() {
		_ = oldDB.Close()
		_ = newDB.Close()
	})

	oldSvc := &Service{DB: oldDB, MaxRows: 100}
	newSvc := &Service{DB: newDB, MaxRows: 100}
	l.mu.Lock()
	l.db = newDB
	l.svc = newSvc
	l.init = true
	l.mu.Unlock()

	l.invalidateIfCurrent(oldSvc)
	if l.svc != newSvc || l.db != newDB {
		t.Fatal("invalidateIfCurrent closed a newer pool")
	}

	l.invalidateIfCurrent(newSvc)
	if l.svc != nil || l.db != nil {
		t.Fatal("invalidateIfCurrent should close the current pool")
	}
}

func TestLazyServiceInvalidateDoesNotBlockOnSlowClose(t *testing.T) {
	t.Parallel()

	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	var closeOnce sync.Once

	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())
	opens := 0
	l.openDB = func(coresnowflake.ConnectParams) (*sql.DB, error) {
		opens++
		return coresnowflake.OpenDB(coresnowflake.ConnectParams{
			Account: "example", User: "user", Warehouse: "wh", Token: "token",
		})
	}
	l.ping = func(context.Context, *sql.DB) error { return nil }
	l.closeDB = func(db *sql.DB) error {
		closeOnce.Do(func() { close(closeStarted) })
		<-releaseClose
		return db.Close()
	}

	if _, err := l.connectService(context.Background()); err != nil {
		t.Fatalf("initial connect: %v", err)
	}
	failing := l.svc

	done := make(chan struct{})
	go func() {
		defer close(done)
		l.invalidateIfCurrent(failing)
	}()

	select {
	case <-closeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected async close to start")
	}

	// While the old pool Close is still blocked, reconnect must proceed
	// (mutex is not held across driver Close).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := l.connectService(ctx); err != nil {
		t.Fatalf("reconnect while old Close blocked: %v", err)
	}
	if opens != 2 {
		t.Fatalf("opens = %d, want 2", opens)
	}

	close(releaseClose)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("invalidateIfCurrent did not return after Close unblocked")
	}
}

func TestLazyServiceCachesConnectTimeoutInitFailure(t *testing.T) {
	t.Parallel()

	opens := 0
	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())
	l.openDB = func(coresnowflake.ConnectParams) (*sql.DB, error) {
		opens++
		db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
			Account:   "example",
			User:      "user",
			Warehouse: "wh",
			Token:     "token",
		})
		if err != nil {
			return nil, err
		}
		return db, nil
	}
	// Simulate the service-owned connect timeout firing while the caller is still active.
	l.ping = func(context.Context, *sql.DB) error {
		return context.DeadlineExceeded
	}

	_, err := l.connectService(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("first connect err = %v, want ErrUnavailable", err)
	}
	if l.initErr == nil {
		t.Fatal("expected service-owned connect timeout to be cached as initErr")
	}

	_, err = l.connectService(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second connect err = %v, want cached ErrUnavailable", err)
	}
	if opens != 1 {
		t.Fatalf("opens = %d, want 1 (cooldown should skip reopen)", opens)
	}
}

func TestLazyServiceDoesNotCacheCallerCanceledInitFailure(t *testing.T) {
	t.Parallel()

	opens := 0
	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())
	l.openDB = func(coresnowflake.ConnectParams) (*sql.DB, error) {
		opens++
		db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
			Account:   "example",
			User:      "user",
			Warehouse: "wh",
			Token:     "token",
		})
		if err != nil {
			return nil, err
		}
		return db, nil
	}
	l.ping = func(context.Context, *sql.DB) error {
		if opens == 1 {
			return context.Canceled
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := l.connectService(ctx)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("first connect err = %v, want ErrUnavailable", err)
	}
	if l.initErr != nil {
		t.Fatalf("caller cancel should not be cached as initErr: %v", l.initErr)
	}

	_, err = l.connectService(context.Background())
	if err != nil {
		t.Fatalf("second connect: %v", err)
	}
	if opens != 2 {
		t.Fatalf("opens = %d, want 2", opens)
	}
}

func TestLazyServiceDoesNotCacheCallerCancelDuringPing(t *testing.T) {
	t.Parallel()

	opens := 0
	l := NewLazyService(coresnowflake.ConnectParams{
		Account:   "example",
		User:      "user",
		Warehouse: "wh",
		Token:     "token",
	}, 100, slog.Default())
	l.openDB = func(coresnowflake.ConnectParams) (*sql.DB, error) {
		opens++
		db, err := coresnowflake.OpenDB(coresnowflake.ConnectParams{
			Account:   "example",
			User:      "user",
			Warehouse: "wh",
			Token:     "token",
		})
		if err != nil {
			return nil, err
		}
		return db, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	l.ping = func(context.Context, *sql.DB) error {
		if opens == 1 {
			cancel()
			return context.Canceled
		}
		return nil
	}

	_, err := l.connectService(ctx)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("connect err = %v, want ErrUnavailable", err)
	}
	if l.initErr != nil {
		t.Fatalf("mid-ping caller cancel should not be cached as initErr: %v", l.initErr)
	}

	_, err = l.connectService(context.Background())
	if err != nil {
		t.Fatalf("second connect: %v", err)
	}
	if opens != 2 {
		t.Fatalf("opens = %d, want 2", opens)
	}
}
