package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type gpuCoverageWarningGroup struct {
	ids  []int
	gaps []TimeRange
}

// Keep JSON coverage per series, but report each identical time gap once. When
// coverage and gaps affect the same cards, combine them into one warning line.
func appendGPUCoverageWarnings(result *Result, coverage *GPUCoverage) {
	type summaryKey struct {
		actual, expected int
		known            bool
		ratio            float64
	}
	summaryGroups := map[summaryKey]*gpuCoverageWarningGroup{}
	summaryOrder := []*gpuCoverageWarningGroup{}
	gapCards := map[TimeRange]map[int]bool{}
	for id, entry := range coverage.Series {
		if entry.Ratio >= 0.9 && len(entry.Gaps) == 0 {
			continue
		}
		key := summaryKey{entry.ActualPoints, entry.ExpectedPoints, entry.RatioKnown, entry.Ratio}
		group := summaryGroups[key]
		if group == nil {
			group = &gpuCoverageWarningGroup{}
			summaryGroups[key] = group
			summaryOrder = append(summaryOrder, group)
		}
		group.ids = append(group.ids, id)
		for _, gap := range entry.Gaps {
			if gapCards[gap] == nil {
				gapCards[gap] = map[int]bool{}
			}
			gapCards[gap][id] = true
		}
	}
	gapGroups := map[string]*gpuCoverageWarningGroup{}
	for gap, cards := range gapCards {
		ids := make([]int, 0, len(cards))
		for id := range cards {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		scope := coverageWarningScope(ids)
		group := gapGroups[scope]
		if group == nil {
			group = &gpuCoverageWarningGroup{ids: ids}
			gapGroups[scope] = group
		}
		group.gaps = append(group.gaps, gap)
	}
	for _, group := range gapGroups {
		sort.Slice(group.gaps, func(i, j int) bool {
			if group.gaps[i].Start != group.gaps[j].Start {
				return group.gaps[i].Start < group.gaps[j].Start
			}
			return group.gaps[i].End < group.gaps[j].End
		})
	}
	label := func(ids []int) string {
		if len(ids) == 1 {
			return coverage.Series[ids[0]].Key
		}
		return fmt.Sprintf("%d 个卡序列", len(ids))
	}
	gapText := func(gaps []TimeRange) string {
		periods := make([]string, 0, len(gaps))
		for _, gap := range gaps {
			periods = append(periods, gap.Start+" 至 "+gap.End)
		}
		return "数据断档（UTC+8）：" + strings.Join(periods, "；")
	}
	for _, group := range summaryOrder {
		entry := coverage.Series[group.ids[0]]
		var text string
		if coverage.IntervalSeconds <= 0 {
			text = fmt.Sprintf("%s 仅有 %d 个采样点，无法确认采样周期和覆盖率", label(group.ids), entry.ActualPoints)
		} else {
			text = fmt.Sprintf("%s 数据覆盖不足：%d/%d 个采样点，覆盖约 %.1f%%", label(group.ids), entry.ActualPoints, entry.ExpectedPoints, 100*entry.Ratio)
		}
		scope := coverageWarningScope(group.ids)
		if gaps := gapGroups[scope]; gaps != nil {
			text += "；" + gapText(gaps.gaps)
			delete(gapGroups, scope)
		}
		result.Warnings = append(result.Warnings, text+"；avg/p95 仅代表实际观测数据（UTC+8）")
	}
	remaining := make([]*gpuCoverageWarningGroup, 0, len(gapGroups))
	for _, group := range gapGroups {
		remaining = append(remaining, group)
	}
	sort.Slice(remaining, func(i, j int) bool {
		if remaining[i].gaps[0].Start != remaining[j].gaps[0].Start {
			return remaining[i].gaps[0].Start < remaining[j].gaps[0].Start
		}
		return coverageWarningScope(remaining[i].ids) < coverageWarningScope(remaining[j].ids)
	})
	for _, group := range remaining {
		result.Warnings = append(result.Warnings, label(group.ids)+" "+gapText(group.gaps))
	}
}

func coverageWarningScope(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}
