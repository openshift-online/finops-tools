package snowflake

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
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
