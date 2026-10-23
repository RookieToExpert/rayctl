package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestGPUCoverageDetectsEdgesAndInternalGapsPerCard(t *testing.T) {
	options := QueryOptions{Start: time.Unix(1000, 0), End: time.Unix(1600, 0), UseStep: true, Step: 15 * time.Second}
	full := &pointSeries{name: "full", points: map[int64]float64{}}
	partial := &pointSeries{name: "partial", points: map[int64]float64{}}
	for i := 1000; i <= 1600; i += 15 {
		full.points[int64(i)*1000] = 1
		if (i >= 1120 && i <= 1210) || (i >= 1420 && i <= 1480) {
			partial.points[int64(i)*1000] = 1
		}
	}
	r := &Result{}
	appendGPUCoverage(r, options, []*pointSeries{full, partial})
	if r.Coverage == nil || len(r.Coverage.Series) != 2 || r.Coverage.Series[0].Ratio != 1 {
		t.Fatalf("%#v", r.Coverage)
	}
	p := r.Coverage.Series[1]
	if len(p.Gaps) != 3 || p.Ratio >= 0.5 || p.ActualPoints != 12 || p.ExpectedPoints != 41 {
		t.Fatalf("%#v", p)
	}
	if len(r.Warnings) == 0 || !strings.Contains(strings.Join(r.Warnings, " "), "UTC+8") {
		t.Fatal(r.Warnings)
	}
	if !strings.Contains(p.Gaps[0].Start, "+08:00") {
		t.Fatal(p.Gaps)
	}
}

func TestGPUCoverageEmptyAndUnknownInterval(t *testing.T) {
	options := QueryOptions{Start: time.Unix(1000, 0), End: time.Unix(1600, 0)}
	r := &Result{}
	appendGPUCoverage(r, options, nil)
	if r.Coverage.MinimumRatio != 0 || len(r.Coverage.Series[0].Gaps) != 1 {
		t.Fatal(r.Coverage)
	}
	r = &Result{}
	appendGPUCoverage(r, options, []*pointSeries{{name: "single", points: map[int64]float64{1200000: 1}}})
	if r.Coverage.IntervalSeconds != 0 || len(r.Warnings) == 0 {
		t.Fatal(r.Coverage)
	}
}
