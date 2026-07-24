// Package parallel provides bounded concurrent execution helpers.
package parallel

import (
	"context"

	"golang.org/x/sync/errgroup"
)

const (
	// DefaultWorkers is the default concurrency when Workers is unset (0).
	DefaultWorkers = 25
	// MaxWorkers is the upper bound for concurrent workers.
	MaxWorkers = 1000
)

// WorkersOrDefault returns n when positive, otherwise DefaultWorkers.
// Values above MaxWorkers are clamped to MaxWorkers.
func WorkersOrDefault(n int) int {
	if n <= 0 {
		return DefaultWorkers
	}
	if n > MaxWorkers {
		return MaxWorkers
	}
	return n
}

// ForEach runs fn for each index in [0, count) with at most workers concurrent calls.
// When workers is 1, fn runs sequentially without spawning goroutines.
// The first error cancels remaining work via context.
func ForEach(ctx context.Context, workers, count int, fn func(ctx context.Context, i int) error) error {
	if count <= 0 {
		return nil
	}
	workers = WorkersOrDefault(workers)
	if workers > count {
		workers = count
	}
	if workers == 1 {
		for i := 0; i < count; i++ {
			if err := fn(ctx, i); err != nil {
				return err
			}
		}
		return nil
	}

	sem := make(chan struct{}, workers)
	g, gctx := errgroup.WithContext(ctx)
	for i := 0; i < count; i++ {
		i := i
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-gctx.Done():
				return gctx.Err()
			}
			return fn(gctx, i)
		})
	}
	return g.Wait()
}
