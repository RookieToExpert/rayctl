package concurrent

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestMapStopsDispatchAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	results := Map(ctx, []int{1, 2, 3, 4}, 1, func(_ context.Context, value int) int {
		calls.Add(1)
		cancel()
		return value
	})
	if calls.Load() != 1 || results[0] != 1 || results[1] != 0 || results[2] != 0 {
		t.Fatalf("calls = %d, results = %v", calls.Load(), results)
	}
}
