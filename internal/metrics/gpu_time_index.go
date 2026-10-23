package metrics

import "sort"

type gpuTimeIndex struct {
	timestamps []int64
	values     map[int64]float64
}

func newGPUTimeIndex(points map[int64]float64) *gpuTimeIndex {
	return &gpuTimeIndex{timestamps: sortedPointTimestamps(points), values: points}
}

func (index *gpuTimeIndex) nearby(at int64) *float64 {
	position := sort.Search(len(index.timestamps), func(i int) bool { return index.timestamps[i] >= at })
	best := -1
	distance := int64(120001)
	// Earlier wins an exact tie, independent of Go's map iteration order.
	for _, i := range []int{position - 1, position} {
		if i < 0 || i >= len(index.timestamps) {
			continue
		}
		delta := index.timestamps[i] - at
		if delta < 0 {
			delta = -delta
		}
		if delta < distance {
			best = i
			distance = delta
		}
	}
	if best < 0 {
		return nil
	}
	value := index.values[index.timestamps[best]]
	return &value
}
