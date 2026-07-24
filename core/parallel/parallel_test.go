package parallel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkersOrDefault(t *testing.T) {
	if got := WorkersOrDefault(0); got != DefaultWorkers {
		t.Fatalf("WorkersOrDefault(0) = %d, want %d", got, DefaultWorkers)
	}
	if got := WorkersOrDefault(-1); got != DefaultWorkers {
		t.Fatalf("WorkersOrDefault(-1) = %d, want %d", got, DefaultWorkers)
	}
	if got := WorkersOrDefault(3); got != 3 {
		t.Fatalf("WorkersOrDefault(3) = %d, want 3", got)
	}
	if got := WorkersOrDefault(MaxWorkers); got != MaxWorkers {
		t.Fatalf("WorkersOrDefault(%d) = %d, want %d", MaxWorkers, got, MaxWorkers)
	}
	if got := WorkersOrDefault(MaxWorkers + 1); got != MaxWorkers {
		t.Fatalf("WorkersOrDefault(%d) = %d, want %d", MaxWorkers+1, got, MaxWorkers)
	}
}

func TestForEachSequential(t *testing.T) {
	var sum int
	err := ForEach(context.Background(), 1, 5, func(_ context.Context, i int) error {
		sum += i
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum != 10 {
		t.Fatalf("sum = %d, want 10", sum)
	}
}

func TestForEachPropagatesError(t *testing.T) {
	err := ForEach(context.Background(), 4, 8, func(_ context.Context, i int) error {
		if i == 3 {
			return errors.New("boom")
		}
		return nil
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestForEachRespectsWorkerLimit(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32
	err := ForEach(context.Background(), 2, 6, func(ctx context.Context, _ int) error {
		cur := atomic.AddInt32(&concurrent, 1)
		for {
			prev := atomic.LoadInt32(&maxConcurrent)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxConcurrent, prev, cur) {
				break
			}
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		atomic.AddInt32(&concurrent, -1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if maxConcurrent > 2 {
		t.Fatalf("max concurrent = %d, want <= 2", maxConcurrent)
	}
}

func TestForEachEmpty(t *testing.T) {
	if err := ForEach(context.Background(), 10, 0, func(context.Context, int) error {
		t.Fatal("fn should not run")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestForEachNegativeCount(t *testing.T) {
	if err := ForEach(context.Background(), 10, -1, func(context.Context, int) error {
		t.Fatal("fn should not run")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
