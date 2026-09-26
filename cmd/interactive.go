package cmd

import (
	"bufio"
	"context"
	"io"
)

type inputResult struct {
	value string
	err   error
}

func readLineWithContext(ctx context.Context, input io.Reader) (string, error) {
	result := make(chan inputResult, 1)
	go func() {
		line, err := bufio.NewReader(input).ReadString('\n')
		result <- inputResult{value: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case read := <-result:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return read.value, read.err
	}
}

func readAllWithContext(ctx context.Context, input io.Reader) ([]byte, error) {
	type result struct {
		value []byte
		err   error
	}
	completed := make(chan result, 1)
	go func() {
		value, err := io.ReadAll(input)
		completed <- result{value: value, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case read := <-completed:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return read.value, read.err
	}
}
