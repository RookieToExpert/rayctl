package metrics

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

type gpuVendor struct {
	hardware   string
	dimensions map[string][]string
}

// These are exporter metric names, not resource totals. Empty dimensions are
// explicitly unsupported. NVIDIA health names follow NVIDIA's DCGM collector.
var gpuVendors = []gpuVendor{
	{"mx.com/gpu", map[string][]string{
		"util": {"mx_gpu_usage"}, "mem_used": {"mx_memory_used"}, "mem_total": {"mx_memory_total"},
		"ras": {"mx_ras_count"}, "ecc": {"mx_ecc_error_count"}, "error_code": nil, "isolated_pages": nil, "power": nil, "temp": nil,
	}},
	{"nvidia.com/gpu", map[string][]string{
		"util": leptonMetrics("gpu_util"), "mem_used": leptonMetrics("gpu_memory_used__MiB"),
		"mem_total": append(append(leptonMetrics("gpu_memory_total__MiB"), leptonMetrics("gpu_memory_total__GiB")...), leptonMetrics("gpu_memory_total__bytes")...),
		"ras":       nil, "ecc": {"DCGM_FI_DEV_ECC_SBE_VOL_TOTAL", "DCGM_FI_DEV_ECC_DBE_VOL_TOTAL"},
		"error_code": {"DCGM_FI_DEV_XID_ERRORS"}, "isolated_pages": nil,
		"power": leptonMetrics("gpu_power_usage"), "temp": leptonMetrics("gpu_temp"),
	}},
	{"npu.com/gpu", map[string][]string{
		"util": {"npu_chip_info_utilization"}, "mem_used": {"container_npu_used_memory", "npu_chip_info_hbm_used_memory"},
		"mem_total": {"container_npu_total_memory", "npu_chip_info_hbm_total_memory"}, "ras": nil,
		"ecc":        {"npu_chip_info_hbm_ecc_single_bit_error_cnt", "npu_chip_info_hbm_ecc_double_bit_error_cnt"},
		"error_code": {"npu_chip_info_error_code"}, "isolated_pages": {"npu_chip_info_hbm_ecc_single_bit_isolated_pages_cnt", "npu_chip_info_hbm_ecc_double_bit_isolated_pages_cnt"}, "power": nil, "temp": nil,
	}},
}

func leptonMetrics(suffix string) []string {
	names := make([]string, 0, 4)
	for _, product := range []string{"acn", "ait", "ecp", "ssp"} {
		names = append(names, "lepton__"+product+"__"+suffix)
	}
	return names
}

func (s *Service) GPU(ctx context.Context, options QueryOptions) (*Result, error) {
	if options.Environment == "" {
		options.Environment = s.environment
	}
	identity, err := s.resolveWorkload(ctx, options)
	if err != nil {
		return nil, err
	}
	selectedPods := append([]string{}, options.Pods...)
	result, err := s.gpuForIdentity(ctx, applyIdentity(options, identity), identity)
	if err != nil {
		return nil, err
	}
	if len(selectedPods) > 0 && len(identity.AllPods) > len(identity.Pods) {
		all := identity
		all.Pods = identity.AllPods
		baselineOptions := applyIdentity(options, all)
		baselineOptions.Health, baselineOptions.By = false, "pod"
		baseline, baselineErr := s.gpuForIdentity(ctx, baselineOptions, all)
		if baselineErr != nil {
			result.Warnings = append(result.Warnings, "全体基线查询失败: "+baselineErr.Error())
		} else {
			result.Baseline = gpuBaseline(baseline, result)
		}
	}
	return result, nil
}

func (s *Service) gpuForIdentity(ctx context.Context, options QueryOptions, identity workloadIdentity) (*Result, error) {
	names := []string{}
	for _, vendor := range gpuVendors {
		names = append(names, vendor.dimensions["util"]...)
	}
	raw, err := s.queryGPU(ctx, names, options, nil, false)
	if err != nil {
		return nil, err
	}
	raw = canonicalGPUInventory(raw)
	merged := mergeRawSeries(raw, acceleratorIdentity, "percent")
	tolerance := 5 * time.Second
	// query_range already aligns every series to the requested grid. Merging
	// adjacent timestamps here would silently discard a user's short --step.
	if options.UseStep {
		tolerance = 0
	}
	// Collapse duplicate exporter timestamps per card BEFORE summing memory.
	for _, card := range merged {
		card.points = coalesceNearbyPoints(card.points, aggregateMax, tolerance)
	}
	grouped := aggregateSeriesWithTolerance(merged, options.By, aggregateMax, tolerance)
	result := buildResult("gpu", options, identity, grouped)
	result.Warnings = append(result.Warnings, identity.Warnings...)
	if len(result.Samples) == 0 {
		return nil, fmt.Errorf("工作负载 %q 没有加速卡指标", options.Workload)
	}
	allHealth := map[string][]RawSeries{}
	for _, vendor := range gpuVendors {
		cards := []RawSeries{}
		for _, item := range raw {
			if item.Metric["extended_resource__type"] == vendor.hardware {
				cards = append(cards, item)
			}
		}
		if len(cards) == 0 {
			continue
		}
		capability := GPUCapability{Hardware: vendor.hardware, Dimensions: map[string]string{}}
		for dimension, metricNames := range vendor.dimensions {
			capability.Dimensions[dimension] = "unsupported"
			if len(metricNames) > 0 {
				capability.Dimensions[dimension] = "mapped"
			}
		}
		capability.Dimensions["util"] = "available"
		memory := map[string][]*pointSeries{}
		for _, dimension := range []string{"mem_used", "mem_total", "ras", "ecc", "error_code", "isolated_pages"} {
			if !options.Health && dimension != "mem_used" && dimension != "mem_total" {
				continue
			}
			metricNames := vendor.dimensions[dimension]
			if len(metricNames) == 0 {
				continue
			}
			values, queryErr := s.queryGPU(ctx, metricNames, options, cards, dimension != "mem_used")
			if queryErr != nil {
				capability.Dimensions[dimension] = "query_failed"
				result.Warnings = append(result.Warnings, vendor.hardware+" "+dimension+": "+queryErr.Error())
				continue
			}
			capability.Dimensions[dimension] = "no_data"
			if len(values) > 0 {
				capability.Dimensions[dimension] = "available"
			}
			if strings.HasPrefix(dimension, "mem_") {
				for i := range values {
					for j := range values[i].Values {
						values[i].Values[j] *= gpuMemoryGiBScale(values[i].Metric["__name__"])
					}
				}
				points := mergeRawSeries(values, acceleratorIdentity, "GiB")
				for _, card := range points {
					card.points = coalesceNearbyPoints(card.points, aggregateMax, tolerance)
				}
				memory[dimension] = aggregateSeriesWithTolerance(points, options.By, aggregateSum, tolerance)
			} else {
				allHealth[dimension] = append(allHealth[dimension], values...)
			}
		}
		attachGPUMemory(result, memory["mem_used"], memory["mem_total"])
		result.Capabilities = append(result.Capabilities, capability)
	}
	if options.Health {
		result.Health = gpuHealth(raw, allHealth)
	}
	return result, nil
}

func metricNamesMatcher(names []string) map[string]string {
	escaped := make([]string, 0, len(names))
	for _, name := range names {
		escaped = append(escaped, regexp.QuoteMeta(name))
	}
	return map[string]string{"__name__=~": "^(?:" + strings.Join(escaped, "|") + ")$"}
}

func (s *Service) queryGPU(ctx context.Context, names []string, options QueryOptions, cards []RawSeries, nodeFallback bool) ([]RawSeries, error) {
	result := []RawSeries{}
	for _, podLabel := range []string{"pod", "exported_pod"} {
		matchers := metricNamesMatcher(names)
		for key, value := range podMatchers(options.Pods) {
			matchers[strings.Replace(key, "pod", podLabel, 1)] = value
		}
		if strings.HasPrefix(names[0], "mx_memory_") {
			matchers["type"] = "vram"
		}
		values, err := s.queryMetric(ctx, "", matchers, options)
		if err != nil {
			return nil, err
		}
		result = append(result, normalizeGPU(values, cards)...)
	}
	if nodeFallback && len(result) == 0 {
		nodes := map[string]struct{}{}
		for _, card := range cards {
			if node := card.Metric["node"]; node != "" {
				nodes[node] = struct{}{}
			}
		}
		if len(nodes) > 0 {
			for _, nodeLabel := range []string{"Hostname", "node"} {
				matchers := metricNamesMatcher(names)
				matchers[nodeLabel+"=~"] = "^(?:" + strings.Join(escapedKeys(nodes), "|") + ")$"
				if strings.HasPrefix(names[0], "mx_memory_") {
					matchers["type"] = "vram"
				}
				values, err := s.queryMetric(ctx, "", matchers, options)
				if err != nil {
					return nil, err
				}
				result = append(result, normalizeGPU(values, cards)...)
			}
		}
	}
	return result, nil
}

func escapedKeys(values map[string]struct{}) []string {
	result := sortedKeys(values)
	for i := range result {
		result[i] = regexp.QuoteMeta(result[i])
	}
	return result
}

func normalizeGPU(raw []RawSeries, cards []RawSeries) []RawSeries {
	result := []RawSeries{}
	for _, item := range raw {
		labels := cloneLabels(item.Metric)
		name := labels["__name__"]
		labels["pod"] = metricLabel(labels, "exported_pod", "pod", "pod_name")
		labels["container"] = metricLabel(labels, "exported_container", "container", "container_name")
		labels["node"] = metricLabel(labels, "Hostname", "node", "hostname")
		labels["gpu"] = metricLabel(labels, "gpu", "device", "minor_number", "id", "chip_id", "device_id")
		labels["uuid"] = metricLabel(labels, "uuid", "UUID")
		labels["model"] = metricLabel(labels, "modelName", "model_name", "model")
		switch {
		case strings.HasPrefix(name, "mx_"):
			labels["extended_resource__type"] = "mx.com/gpu"
			labels["container"] = metricLabel(labels, "exported_container")
		case strings.HasPrefix(name, "npu_") || strings.HasPrefix(name, "container_npu_"):
			labels["extended_resource__type"] = "npu.com/gpu"
		case labels["extended_resource__type"] == "":
			labels["extended_resource__type"] = "nvidia.com/gpu"
		}
		item.Metric, item.Values = labels, append([]float64{}, item.Values...)
		if len(cards) == 0 {
			result = append(result, item)
			continue
		}
		// Node-level totals/health must only attach to physical cards belonging
		// to this workload, never to an unrelated card on the same host.
		for _, card := range cards {
			if !sameGPU(labels, card.Metric) {
				continue
			}
			copyItem := item
			copyItem.Metric = cloneLabels(labels)
			for _, key := range []string{"pod", "container", "node", "gpu", "extended_resource__type"} {
				copyItem.Metric[key] = card.Metric[key]
			}
			for _, key := range []string{"uuid", "model"} {
				if copyItem.Metric[key] == "" {
					copyItem.Metric[key] = card.Metric[key]
				}
			}
			result = append(result, copyItem)
			break
		}
	}
	return result
}

func sameGPU(a, b map[string]string) bool {
	if a["extended_resource__type"] != b["extended_resource__type"] {
		return false
	}
	if a["node"] != "" && b["node"] != "" && a["node"] != b["node"] {
		return false
	}
	if a["uuid"] != "" && b["uuid"] != "" {
		return a["uuid"] == b["uuid"]
	}
	if a["gpu"] == "" || a["gpu"] != b["gpu"] {
		return false
	}
	return (a["pod"] != "" && a["pod"] == b["pod"]) || (a["node"] != "" && a["node"] == b["node"])
}

// Platform wrappers and native exporters may expose the same physical GPU
// with different container labels. Canonicalize before grouping or summing.
func canonicalGPUInventory(raw []RawSeries) []RawSeries {
	groups := map[string]map[string]string{}
	for _, item := range raw {
		key := item.Metric["node"] + "/" + item.Metric["pod"] + "/" + item.Metric["gpu"] + "/" + item.Metric["extended_resource__type"]
		if item.Metric["gpu"] == "" {
			key += "/" + item.Metric["uuid"]
		}
		target := groups[key]
		if target == nil {
			target = cloneLabels(item.Metric)
			groups[key] = target
		}
		for _, field := range []string{"container", "node", "uuid", "model"} {
			if target[field] == "" {
				target[field] = item.Metric[field]
			}
		}
	}
	for i := range raw {
		key := raw[i].Metric["node"] + "/" + raw[i].Metric["pod"] + "/" + raw[i].Metric["gpu"] + "/" + raw[i].Metric["extended_resource__type"]
		if raw[i].Metric["gpu"] == "" {
			key += "/" + raw[i].Metric["uuid"]
		}
		for _, field := range []string{"container", "node", "uuid", "model"} {
			raw[i].Metric[field] = groups[key][field]
		}
	}
	return raw
}

func gpuMemoryGiBScale(name string) float64 {
	switch {
	case strings.HasPrefix(name, "mx_"):
		return 1.0 / (1024 * 1024)
	case strings.HasSuffix(name, "__GiB"):
		return 1
	case strings.HasSuffix(name, "__bytes"):
		return 1.0 / (1024 * 1024 * 1024)
	default:
		return 1.0 / 1024
	}
}

func attachGPUMemory(result *Result, used, total []*pointSeries) {
	usedByName, totalByName := map[string]*pointSeries{}, map[string]*pointSeries{}
	for _, item := range used {
		usedByName[item.name] = item
	}
	for _, item := range total {
		totalByName[item.name] = item
	}
	targets := map[string]*Series{}
	indexes := map[*pointSeries]*gpuTimeIndex{}
	for _, set := range [][]*pointSeries{used, total} {
		for _, item := range set {
			indexes[item] = newGPUTimeIndex(item.points)
		}
	}
	for i := range result.Series {
		targets[result.Series[i].Name] = &result.Series[i]
	}
	for i := range result.Samples {
		sample := &result.Samples[i]
		at, _ := time.Parse(time.RFC3339Nano, sample.Timestamp)
		if item := usedByName[sample.Series]; item != nil {
			sample.MemoryUsedGiB = indexes[item].nearby(at.UnixMilli())
		}
		if item := totalByName[sample.Series]; item != nil {
			sample.MemoryTotalGiB = indexes[item].nearby(at.UnixMilli())
		}
		if sample.MemoryUsedGiB != nil && sample.MemoryTotalGiB != nil && *sample.MemoryTotalGiB > 0 {
			value := *sample.MemoryUsedGiB / *sample.MemoryTotalGiB * 100
			sample.MemoryPercent = &value
		}
		if target := targets[sample.Series]; target != nil && sample.MemoryUsedGiB != nil {
			target.MemoryUsedGiB, target.MemoryTotalGiB, target.MemoryPercent = sample.MemoryUsedGiB, sample.MemoryTotalGiB, sample.MemoryPercent
			target.MemoryUsedPeakMiB = math.Max(target.MemoryUsedPeakMiB, *sample.MemoryUsedGiB*1024)
			if sample.MemoryTotalGiB != nil {
				target.MemoryTotalMiB = *sample.MemoryTotalGiB * 1024
			}
		}
	}

}

func nearbyGPUValue(points map[int64]float64, at int64) *float64 {
	return newGPUTimeIndex(points).nearby(at)
}

func completeSeriesSummary(summary *SeriesSummary, points map[int64]float64) {
	values := []float64{}
	nonzero := 0
	sum := 0.0
	for _, value := range points {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		values = append(values, value)
		sum += value
		if value != 0 {
			nonzero++
		}
	}
	if len(values) == 0 {
		return
	}
	sort.Float64s(values)
	summary.Average = sum / float64(len(values))
	summary.P95 = quantile(values, .95)
	summary.NonzeroPercent = float64(nonzero) * 100 / float64(len(values))
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := q * float64(len(sorted)-1)
	lo := int(math.Floor(index))
	hi := int(math.Ceil(index))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(index-float64(lo))
}

func physicalGPUKey(labels map[string]string) string {
	if labels["uuid"] != "" {
		return labels["node"] + "/" + labels["uuid"]
	}
	return labels["node"] + "/" + labels["pod"] + "/" + labels["gpu"] + "/" + labels["extended_resource__type"]
}

func gpuHealth(cards []RawSeries, dimensions map[string][]RawSeries) *GPUHealth {
	known := map[string]GPUHealthCard{}
	labelsByKey := map[string]map[string]string{}
	for _, card := range cards {
		key := physicalGPUKey(card.Metric)
		labelsByKey[key] = card.Metric
		known[key] = GPUHealthCard{Node: card.Metric["node"], GPU: card.Metric["gpu"], UUID: card.Metric["uuid"], Model: card.Metric["model"], Pod: card.Metric["pod"]}
	}
	health := &GPUHealth{TotalCards: len(known), Anomalies: []GPUHealthCard{}}
	keys := make([]string, 0, len(known))
	for key := range known {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		card := known[key]
		for _, dimension := range []string{"ras", "ecc", "error_code", "isolated_pages"} {
			// Deduplicate each metric independently, then combine distinct ECC
			// single/double counters; never sum the three sco_srm_type views.
			maxima := map[string]float64{}
			for _, series := range dimensions[dimension] {
				if !sameGPU(series.Metric, labelsByKey[key]) {
					continue
				}
				name := series.Metric["__name__"]
				for _, value := range series.Values {
					if math.IsNaN(value) || math.IsInf(value, 0) {
						continue
					}
					if old, ok := maxima[name]; !ok || value > old {
						maxima[name] = value
					}
				}
				if card.UUID == "" {
					card.UUID = series.Metric["uuid"]
				}
				if card.Model == "" {
					card.Model = series.Metric["model"]
				}
			}
			if len(maxima) == 0 {
				continue
			}
			value := 0.0
			for _, v := range maxima {
				value += v
			}
			switch dimension {
			case "ras":
				card.RAS = &value
			case "ecc":
				card.ECC = &value
			case "error_code":
				card.ErrorCode = &value
			case "isolated_pages":
				card.IsolatedPages = &value
			}
		}
		bad := false
		for _, v := range []*float64{card.RAS, card.ECC, card.ErrorCode, card.IsolatedPages} {
			if v != nil && *v > 0 {
				bad = true
			}
		}
		if bad {
			health.Anomalies = append(health.Anomalies, card)
		} else if card.ECC != nil {
			health.HealthyCards++
		} else {
			health.UnknownCards++
		}
	}
	return health
}

func gpuBaseline(all, selected *Result) *GPUBaseline {
	baseline := &GPUBaseline{PodCount: len(all.Series)}
	utils, memories := []float64{}, []float64{}
	for _, series := range all.Series {
		utils = append(utils, series.Summary.Latest)
		if series.MemoryUsedGiB != nil {
			memories = append(memories, *series.MemoryUsedGiB)
		}
	}
	sort.Float64s(utils)
	sort.Float64s(memories)
	baseline.UtilP50, baseline.UtilP95 = quantile(utils, .5), quantile(utils, .95)
	if len(utils) > 0 {
		baseline.UtilMax = utils[len(utils)-1]
	}
	if len(memories) > 0 {
		p50, p95, min := quantile(memories, .5), quantile(memories, .95), memories[0]
		baseline.MemoryP50GiB, baseline.MemoryP95GiB, baseline.MemoryMinGiB = &p50, &p95, &min
		byPod := map[string]float64{}
		for _, series := range selected.Series {
			if series.Pod != nil && series.MemoryUsedGiB != nil {
				byPod[*series.Pod] += *series.MemoryUsedGiB
			}
		}
		for _, value := range byPod {
			rank := 0.0
			for _, v := range memories {
				if v < value {
					rank++
				} else if v == value {
					rank += .5
				}
			}
			rank = rank * 100 / float64(len(memories))
			if baseline.SelectedMemoryPercentile == nil || rank < *baseline.SelectedMemoryPercentile {
				v := rank
				baseline.SelectedMemoryPercentile = &v
			}
			if rank <= 5 && value < p50*.5 {
				baseline.SignificantlyLow = true
			}
		}
	}
	return baseline
}
