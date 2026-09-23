package concurrent

import (
	"context"
	"sync"
)

// Map runs at most maxParallel calls and keeps results in input order.
// Every input is called, including after cancellation, so callers receive a
// result for every position. Calls should honor the supplied context.
func Map[I any, R any](ctx context.Context, values []I, maxParallel int, call func(context.Context, I) R) []R {
	results := make([]R, len(values))
	if len(values) == 0 {
		return results
	}
	if maxParallel < 1 {
		maxParallel = 1
	}
	if maxParallel > len(values) {
		maxParallel = len(values)
	}

	indexes := make(chan int)
	var workers sync.WaitGroup
	workers.Add(maxParallel)
	for range maxParallel {
		go func() {
			defer workers.Done()
			for index := range indexes {
				results[index] = call(ctx, values[index])
			}
		}()
	}
	for index := range values {
		indexes <- index
	}
	close(indexes)
	workers.Wait()
	return results
}
