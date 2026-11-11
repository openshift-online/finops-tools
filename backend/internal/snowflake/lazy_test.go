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
