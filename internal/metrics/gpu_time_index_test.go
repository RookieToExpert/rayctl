package metrics

import (
	"fmt"
	"math"
	"testing"
)

func TestGPUTimeIndexMatchesLongWindow(t *testing.T) {
	for _, hours := range []int{72, 168} {
		points := map[int64]float64{}
		for i := 0; i < hours*3600/15; i++ {
			points[int64(i)*15000] = float64(i % 113)
		}
		index := newGPUTimeIndex(points)
		for i := 0; i < hours*3600/15; i++ {
			// A unique nearest point, with jitter on either side of the timestamp.
			at := int64(i)*15000 + int64(i%2001) - 1000
			got := index.nearby(at)
			if got == nil || *got != float64(i%113) {
				t.Fatalf("%dh point %d: %v", hours, i, got)
			}
		}
	}
	index := newGPUTimeIndex(map[int64]float64{0: 1, 2000: 2})
	if got := index.nearby(1000); got == nil || *got != 1 {
		t.Fatalf("tie: %v", got)
	}
	if index.nearby(122001) != nil {
		t.Fatal("outside 120s tolerance")
	}
	if newGPUTimeIndex(nil).nearby(0) != nil {
		t.Fatal("empty index")
	}
	if got := index.nearby(122000); got == nil || *got != 2 {
		t.Fatal("inclusive tolerance lost")
	}
}

func BenchmarkGPUMemoryTimeIndex(b *testing.B) {
	for _, hours := range []int{72, 168} {
		b.Run(fmt.Sprintf("%dh", hours), func(b *testing.B) {
			points := map[int64]float64{}
			for i := 0; i < hours*3600/15; i++ {
				points[int64(i)*15000] = math.Sin(float64(i))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				index := newGPUTimeIndex(points)
				for i := 0; i < len(points); i++ {
					_ = index.nearby(int64(i)*15000 + 500)
				}
			}
		})
	}
}
