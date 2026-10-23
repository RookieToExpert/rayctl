package metrics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGPUIdenticalCoverageAndGapProduceOneWarning(t *testing.T) {
	options := QueryOptions{Start: time.Unix(1000, 0), End: time.Unix(1600, 0), UseStep: true, Step: 15 * time.Second}
	cards := []*pointSeries{{name: "gpu-a", points: map[int64]float64{}}, {name: "gpu-b", points: map[int64]float64{}}}
	for i := 1300; i <= 1600; i += 15 {
		for _, card := range cards {
			card.points[int64(i)*1000] = 1
		}
	}
	result := &Result{}
	appendGPUCoverage(result, options, cards)
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "2 个卡序列") || strings.Count(result.Warnings[0], "数据断档") != 1 {
		t.Fatal(result.Warnings)
	}
	if len(result.Coverage.Series) != 2 || len(result.Coverage.Series[0].Gaps) != 1 || len(result.Coverage.Series[1].Gaps) != 1 {
		t.Fatalf("per-series evidence lost: %+v", result.Coverage)
	}
	encoded, _ := json.Marshal(result.Coverage)
	if strings.Count(string(encoded), `"gaps"`) != 2 {
		t.Fatal(string(encoded))
	}
}

func TestGPUSharedGapIsNotRepeatedWhenRatiosDiffer(t *testing.T) {
	gap := TimeRange{Start: "2026-10-23T10:00:00+08:00", End: "2026-10-23T11:00:00+08:00"}
	coverage := &GPUCoverage{IntervalSeconds: 15, Series: []GPUCoverageSeries{{Key: "a", ActualPoints: 2, ExpectedPoints: 4, Ratio: .5, RatioKnown: true, Gaps: []TimeRange{gap}}, {Key: "b", ActualPoints: 3, ExpectedPoints: 4, Ratio: .75, RatioKnown: true, Gaps: []TimeRange{gap}}}}
	result := &Result{}
	appendGPUCoverageWarnings(result, coverage)
	text := strings.Join(result.Warnings, "\n")
	if strings.Count(text, gap.Start) != 1 || strings.Count(text, "数据断档") != 1 || !strings.Contains(text, "2 个卡序列") {
		t.Fatal(text)
	}
}

func TestGPUCommonAndCardSpecificGapsRemainDistinct(t *testing.T) {
	common := TimeRange{Start: "common-start", End: "common-end"}
	specific := TimeRange{Start: "specific-start", End: "specific-end"}
	coverage := &GPUCoverage{IntervalSeconds: 15, Series: []GPUCoverageSeries{{Key: "a", ActualPoints: 2, ExpectedPoints: 4, Ratio: .5, Gaps: []TimeRange{common, specific}}, {Key: "b", ActualPoints: 2, ExpectedPoints: 4, Ratio: .5, Gaps: []TimeRange{common}}}}
	var expected string
	for i := 0; i < 10; i++ {
		result := &Result{}
		appendGPUCoverageWarnings(result, coverage)
		text := strings.Join(result.Warnings, "\n")
		if strings.Count(text, common.Start) != 1 || strings.Count(text, specific.Start) != 1 {
			t.Fatal(text)
		}
		if i > 0 && text != expected {
			t.Fatal("unstable warning order")
		}
		expected = text
	}
}
