package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoundedMapRunsConcurrentlyAndKeepsInputOrder(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	values := []int{3, 1, 4, 2}

	results := boundedMap(context.Background(), values, 3, func(_ context.Context, value int) int {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		return value * 2
	})

	if maxActive.Load() != 3 {
		t.Fatalf("boundedMap max concurrency = %d, want 3", maxActive.Load())
	}
	for index, want := range []int{6, 2, 8, 4} {
		if results[index] != want {
			t.Fatalf("results[%d] = %d, want %d", index, results[index], want)
		}
	}
}
