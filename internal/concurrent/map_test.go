package concurrent

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestMapFillsResultsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	results := Map(ctx, []int{1, 2, 3, 4}, 1, func(_ context.Context, value int) int {
		calls.Add(1)
		cancel()
		return value
	})
	if calls.Load() != 4 || results[0] != 1 || results[1] != 2 || results[2] != 3 || results[3] != 4 {
		t.Fatalf("calls = %d, results = %v", calls.Load(), results)
	}
}

func TestMapCallsEveryInputWhenAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := Map(ctx, []int{1, 2, 3}, 2, func(ctx context.Context, value int) int {
		if ctx.Err() == nil {
			t.Fatal("expected canceled context")
		}
		return value
	})
	for i, value := range results {
		if value != i+1 {
			t.Fatalf("results = %v", results)
		}
	}
}
