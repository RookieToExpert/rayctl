package service

import (
	"context"

	"rayctl/internal/concurrent"
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
	return concurrent.Map(ctx, values, maxParallel, call)
}
