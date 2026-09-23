package concurrent

import (
	"context"
	"sync"
)

// Map runs at most maxParallel calls and keeps results in input order.
// Entries not started before cancellation retain their zero value.
func Map[I any, R any](ctx context.Context, values []I, maxParallel int, call func(context.Context, I) R) []R {
	results := make([]R, len(values))
	if len(values) == 0 || ctx.Err() != nil {
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
				if ctx.Err() != nil {
					return
				}
				results[index] = call(ctx, values[index])
			}
		}()
	}
dispatch:
	for index := range values {
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			break dispatch
		case indexes <- index:
		}
	}
	close(indexes)
	workers.Wait()
	return results
}
