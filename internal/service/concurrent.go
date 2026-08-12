package service

import (
	"context"
	"sync"
)

type asyncResult[T any] struct {
	Value T
	Err   error
}

func asyncCall[T any](ctx context.Context, call func(context.Context) (T, error)) <-chan asyncResult[T] {
	result := make(chan asyncResult[T], 1)
	go func() {
		defer close(result)
		value, err := call(ctx)
		result <- asyncResult[T]{Value: value, Err: err}
	}()
	return result
}

func boundedMap[T any, R any](ctx context.Context, values []T, maxParallel int, call func(context.Context, T) R) []R {
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
