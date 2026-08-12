package cmd

import (
	"context"
	"sync"
)

func runBoundedQueries[T any](
	ctx context.Context,
	identifiers []string,
	maxParallel int,
	query func(context.Context, string) T,
) []T {
	results := make([]T, len(identifiers))
	if len(identifiers) == 0 {
		return results
	}
	if maxParallel < 1 {
		maxParallel = 1
	}
	if maxParallel > len(identifiers) {
		maxParallel = len(identifiers)
	}

	indexes := make(chan int)
	var workers sync.WaitGroup
	workers.Add(maxParallel)
	for range maxParallel {
		go func() {
			defer workers.Done()
			for index := range indexes {
				results[index] = query(ctx, identifiers[index])
			}
		}()
	}
	for index := range identifiers {
		indexes <- index
	}
	close(indexes)
	workers.Wait()
	return results
}
