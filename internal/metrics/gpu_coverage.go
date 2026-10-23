package metrics

import (
	"math"
	"sort"
	"time"
)

func appendGPUCoverage(result *Result, options QueryOptions, cards []*pointSeries) {
	if options.Start.IsZero() || !options.End.After(options.Start) {
		return
	}
	start, end := options.Start.UnixMilli(), options.End.UnixMilli()
	interval := int64(options.Step / time.Millisecond)
	if !options.UseStep || interval <= 0 {
		var deltas []int64
		for _, card := range cards {
			times := sortedPointTimestamps(card.points)
			for i := 1; i < len(times); i++ {
				if delta := times[i] - times[i-1]; delta > 0 {
					deltas = append(deltas, delta)
				}
			}
		}
		if len(deltas) > 0 {
			sort.Slice(deltas, func(i, j int) bool { return deltas[i] < deltas[j] })
			interval = deltas[(len(deltas)-1)/2]
		}
	}
	coverage := &GPUCoverage{Requested: TimeRange{Start: formatTime(options.Start), End: formatTime(options.End)}, IntervalSeconds: float64(interval) / 1000, MinimumRatio: 1, RatioKnown: true, Series: []GPUCoverageSeries{}}
	if len(cards) == 0 {
		cards = []*pointSeries{{name: "workload", points: map[int64]float64{}}}
	}
	for _, card := range cards {
		entry := GPUCoverageSeries{Key: card.name, Gaps: []TimeRange{}}
		var times []int64
		for timestamp, value := range card.points {
			if timestamp >= start && timestamp <= end && !math.IsNaN(value) && !math.IsInf(value, 0) {
				times = append(times, timestamp)
			}
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		entry.ActualPoints = len(times)
		entry.RatioKnown = interval > 0 || len(times) == 0
		coverage.RatioKnown = coverage.RatioKnown && entry.RatioKnown
		if interval > 0 {
			entry.ExpectedPoints = int((end-start)/interval) + 1
			buckets := map[int64]bool{}
			for _, at := range times {
				buckets[(at-start+interval/2)/interval] = true
			}
			entry.Ratio = math.Min(1, float64(len(buckets))/float64(entry.ExpectedPoints))
		}
		gap := func(a, b int64) {
			if b > a {
				entry.Gaps = append(entry.Gaps, TimeRange{Start: formatTimestamp(a), End: formatTimestamp(b)})
			}
		}
		if len(times) == 0 {
			gap(start, end)
		} else {
			entry.Observed = TimeRange{Start: formatTimestamp(times[0]), End: formatTimestamp(times[len(times)-1])}
			if interval == 0 {
				// A lone observation cannot establish a scrape interval.
				gap(start, times[0])
				gap(times[len(times)-1], end)
			} else {
				if times[0]-start > 3*interval {
					gap(start, times[0])
				}
				for i := 1; i < len(times); i++ {
					if times[i]-times[i-1] > 3*interval {
						gap(times[i-1]+interval, times[i])
					}
				}
				if end-times[len(times)-1] > 3*interval {
					gap(times[len(times)-1]+interval, end)
				}
			}
		}
		coverage.MinimumRatio = math.Min(coverage.MinimumRatio, entry.Ratio)
		coverage.Series = append(coverage.Series, entry)
	}
	result.Coverage = coverage
	appendGPUCoverageWarnings(result, coverage)
}
