package service

import "context"

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
