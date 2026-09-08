package metrics

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	workloadNameLabel      = "label_resource_compute_sensecore_cn_workload_name"
	workloadTypeLabel      = "label_resource_compute_sensecore_cn_workload_type"
	workspaceNameLabel     = "label_resource_compute_sensecore_cn_workspace_name"
	workspaceResourceType  = "compute.ssp.v1.workspace"
	memoryMetric           = "container_memory_working_set_bytes"
	memoryLimitMetric      = "kube_pod_container_resource_limits"
	npuMemoryUsedMetric    = "container_npu_used_memory"
	npuMemoryTotalMetric   = "container_npu_total_memory"
	acceleratorMetric      = "lepton__acn__gpu_util"
	acceleratorMemoryUsed  = "lepton__acn__gpu_memory_used__MiB"
	acceleratorMemoryTotal = "lepton__acn__gpu_memory_total__MiB"
	restartMetric          = "kube_pod_container_status_restarts_total"
	terminatedReasonMetric = "kube_pod_container_status_last_terminated_reason"
)

type Service struct {
	client      queryClient
	environment string
	podResolver PodResolver
}

type queryClient interface {
	Export(context.Context, string, time.Time, time.Time) ([]RawSeries, error)
	QueryRange(context.Context, string, time.Time, time.Time, time.Duration) ([]RawSeries, error)
	Query(context.Context, string, time.Time) ([]RawSeries, error)
}

func NewService(client queryClient, environment ...string) *Service {
	value := ""
	if len(environment) > 0 {
		value = strings.TrimSpace(environment[0])
	}
	return &Service{client: client, environment: value}
}

type PodResolution struct {
	Type      string
	Workspace string
	Pods      []string
}

type PodResolver func(context.Context, string, string, string) (PodResolution, error)

func (s *Service) SetPodResolver(resolver PodResolver) {
	if s != nil {
		s.podResolver = resolver
	}
}

type pointSeries struct {
	name          string
	labels        map[string]string
	points        map[int64]float64
	mergedFrom    int
	droppedSeries int
	droppedTypes  []string
	unit          string
	limit         float64
	hardware      string
}

type workloadIdentity struct {
	Type      string
	Workspace string
	Pods      []string
	Warnings  []string
}

func (s *Service) Memory(ctx context.Context, options QueryOptions) (*Result, error) {
	if options.Environment == "" {
		options.Environment = s.environment
	}
	identity, err := s.resolveWorkload(ctx, options)
	if err != nil {
		return nil, err
	}
	options = applyIdentity(options, identity)
	matchers := podMatchers(options.Pods)
	matchers["container!"] = ""
	matchers["container!="] = "POD"
	raw, err := s.queryMetric(ctx, memoryMetric, matchers, options)
	if err != nil {
		return nil, err
	}
	preferred := preferWorkspaceSeries(raw)
	merged := mergeRawSeries(preferred, memoryIdentity, "bytes")
	grouped := aggregateSeries(merged, options.By, aggregateSum)

	limitRaw, limitErr := s.client.Export(ctx, selector(memoryLimitMetric, mergeMatchers(podMatchers(options.Pods), map[string]string{
		"resource":   "memory",
		"unit":       "byte",
		"container!": "",
	})), options.Start, options.End)
	if limitErr == nil {
		limits := aggregateSeries(mergeRawSeries(preferWorkspaceSeries(limitRaw), memoryIdentity, "bytes"), options.By, aggregateSum)
		applyLimits(grouped, limits)
	}
	result := buildResult("mem", options, identity, grouped)
	result.Warnings = append(result.Warnings, identity.Warnings...)
	if limitErr != nil {
		result.Warnings = append(result.Warnings, "未能读取容器内存 limit: "+limitErr.Error())
	}
	if len(result.Samples) == 0 {
		result.Warnings = append(result.Warnings, "查询区间内没有内存采样点")
	}
	return result, nil
}

func (s *Service) GPU(ctx context.Context, options QueryOptions) (*Result, error) {
	if options.Environment == "" {
		options.Environment = s.environment
	}
	identity, err := s.resolveWorkload(ctx, options)
	if err != nil {
		return nil, err
	}
	options = applyIdentity(options, identity)
	raw, err := s.queryMetric(ctx, acceleratorMetric, podMatchers(options.Pods), options)
	if err != nil {
		return nil, err
	}
	preferred := preferWorkspaceSeries(raw)
	merged := mergeRawSeries(preferred, acceleratorIdentity, "percent")
	grouped := merged
	if options.By == "pod" {
		grouped = aggregateSeries(merged, options.By, aggregateMax)
	}
	result := buildResult("gpu", options, identity, grouped)
	result.Warnings = append(result.Warnings, identity.Warnings...)
	if len(result.Samples) == 0 {
		return nil, fmt.Errorf("工作负载 %q 没有加速卡指标", options.Workload)
	}

	memoryUsedMetric := acceleratorMemoryUsed
	memoryTotalMetric := acceleratorMemoryTotal
	if containsHardwareType(merged, "npu.com/gpu") {
		memoryUsedMetric = npuMemoryUsedMetric
		memoryTotalMetric = npuMemoryTotalMetric
	}
	used, usedErr := s.queryMetric(ctx, memoryUsedMetric, podMatchers(options.Pods), options)
	total, totalErr := s.queryMetric(ctx, memoryTotalMetric, podMatchers(options.Pods), options)
	if usedErr == nil && totalErr == nil {
		usedSeries := mergeRawSeries(preferWorkspaceSeries(used), acceleratorIdentity, "MiB")
		totalSeries := mergeRawSeries(preferWorkspaceSeries(total), acceleratorIdentity, "MiB")
		if options.By == "pod" {
			usedSeries = aggregateSeries(usedSeries, options.By, aggregateSum)
			totalSeries = aggregateSeries(totalSeries, options.By, aggregateSum)
		}
		appendAcceleratorMemorySummary(result, usedSeries, totalSeries)
	} else {
		result.Warnings = append(result.Warnings, "未能完整读取加速卡显存指标")
	}
	return result, nil
}

func (s *Service) Restart(ctx context.Context, options QueryOptions) (*Result, error) {
	if options.Environment == "" {
		options.Environment = s.environment
	}
	identity, err := s.resolveWorkload(ctx, options)
	if err != nil {
		return nil, err
	}
	return s.restartForIdentity(ctx, options, identity)
}

// RestartForPods reuses the restart calculation when Kubernetes has already
// resolved the workload identity, avoiding a full memory-series discovery.
func (s *Service) RestartForPods(ctx context.Context, options QueryOptions) (*Result, error) {
	if len(options.Pods) == 0 || options.Workspace == "" || options.Type == "" || options.Type == "auto" {
		return nil, fmt.Errorf("resolved pods, workspace and workload type are required")
	}
	if options.Environment == "" {
		options.Environment = s.environment
	}
	return s.restartForIdentity(ctx, options, workloadIdentity{Type: options.Type, Workspace: options.Workspace, Pods: uniqueSorted(options.Pods)})
}

func (s *Service) restartForIdentity(ctx context.Context, options QueryOptions, identity workloadIdentity) (*Result, error) {
	options = applyIdentity(options, identity)
	baselineStart := options.Start.Add(-5 * time.Minute)
	restartOptions := options
	restartOptions.Start = baselineStart
	restarts, err := s.queryMetric(ctx, restartMetric, podMatchers(options.Pods), restartOptions)
	if err != nil {
		return nil, err
	}
	reasons, reasonErr := s.queryMetric(ctx, terminatedReasonMetric, podMatchers(options.Pods), restartOptions)
	result := buildRestartResult(
		options,
		identity,
		mergeDuplicateRawSeries(preferWorkspaceSeries(restarts)),
		mergeDuplicateRawSeries(preferWorkspaceSeries(reasons)),
	)
	result.Warnings = append(result.Warnings, identity.Warnings...)
	if reasonErr != nil {
		result.Warnings = append(result.Warnings, "未能读取容器终止原因: "+reasonErr.Error())
	}
	return result, nil
}

func (s *Service) Raw(ctx context.Context, query string, start time.Time, end time.Time, step time.Duration, useRange bool) (*Result, error) {
	var (
		raw []RawSeries
		err error
	)
	if useRange {
		raw, err = s.client.QueryRange(ctx, query, start, end, step)
	} else {
		raw, err = s.client.Query(ctx, query, end)
	}
	if err != nil {
		return nil, err
	}
	mode := "instant"
	if useRange {
		mode = "step"
	}
	result := &Result{Metadata: Metadata{
		Command: "raw", Environment: s.environment, Start: formatTime(start), End: formatTime(end), Range: TimeRange{Start: formatTime(start), End: formatTime(end)}, Timezone: "UTC+8", Mode: mode,
		Metric: query, Unit: "unknown", Labels: map[string]string{"query": query},
	}, Series: []Series{}, Samples: []Sample{}, Warnings: []string{}}
	if useRange {
		result.Metadata.Step = stringPointer(step.String())
	}
	for index, item := range raw {
		name := rawSeriesName(item.Metric, index)
		series := Series{Name: name, Labels: cloneLabels(item.Metric), Unit: "unknown", MergedFrom: 1,
			Pod: optionalString(metricLabel(item.Metric, "pod", "pod_name")), Container: optionalString(metricLabel(item.Metric, "container", "container_name")),
			Node: optionalString(metricLabel(item.Metric, "node", "Hostname", "hostname")), GPU: optionalString(metricLabel(item.Metric, "gpu", "device", "minor_number")),
			DroppedSCOSRMTypes: []string{},
		}
		for i := 0; i < len(item.Values) && i < len(item.Timestamps); i++ {
			sample := Sample{Series: name, Timestamp: formatTimestamp(item.Timestamps[i]), Value: item.Values[i]}
			result.Samples = append(result.Samples, sample)
			updateSeriesSummary(&series.Summary, sample)
		}
		result.Series = append(result.Series, series)
	}
	sortSamples(result.Samples)
	result.Summary.SeriesCount = len(result.Series)
	result.Summary.SampleCount = len(result.Samples)
	result.Metadata.SampleInterval = medianSampleInterval(result.Samples)
	result.Metadata.SampleIntervalSeconds = durationSeconds(result.Metadata.SampleInterval)
	appendSamplingWarning(result)
	return result, nil
}

func (s *Service) queryMetric(ctx context.Context, metric string, matchers map[string]string, options QueryOptions) ([]RawSeries, error) {
	expression := selector(metric, matchers)
	if options.UseStep {
		return s.client.QueryRange(ctx, expression, options.Start, options.End, options.Step)
	}
	return s.client.Export(ctx, expression, options.Start, options.End)
}

func (s *Service) resolveWorkload(ctx context.Context, options QueryOptions) (workloadIdentity, error) {
	matchers := map[string]string{
		workloadNameLabel: options.Workload,
		"sco_srm_type":    workspaceResourceType,
		"container!":      "",
	}
	if options.Type != "" && options.Type != "auto" {
		key, value := metricWorkloadTypeMatcher(options.Type)
		matchers[key] = value
	}
	if options.Workspace != "" {
		matchers[workspaceNameLabel] = options.Workspace
	}
	raw, err := s.client.Export(ctx, selector(memoryMetric, matchers), options.Start, options.End)
	if err != nil {
		return workloadIdentity{}, fmt.Errorf("解析工作负载 Pod: %w", err)
	}
	workspaces := map[string]struct{}{}
	types := map[string]struct{}{}
	pods := map[string]struct{}{}
	for _, item := range raw {
		if value := metricLabel(item.Metric, workspaceNameLabel, "workspace_name", "workspace"); value != "" {
			workspaces[value] = struct{}{}
		}
		if value := normalizeMetricWorkloadType(metricLabel(item.Metric, workloadTypeLabel, "workload_type")); value != "" {
			types[value] = struct{}{}
		}
		if value := metricLabel(item.Metric, "pod", "pod_name"); value != "" {
			pods[value] = struct{}{}
		}
	}
	if options.Workspace == "" && len(workspaces) > 1 {
		return workloadIdentity{}, fmt.Errorf("工作负载 %q 存在于多个 workspace (%s)，请使用 -w/--workspace 指定", options.Workload, strings.Join(sortedKeys(workspaces), ", "))
	}
	identity := workloadIdentity{Type: options.Type, Workspace: options.Workspace}
	if identity.Type == "" || identity.Type == "auto" {
		values := sortedKeys(types)
		if len(values) == 1 {
			identity.Type = values[0]
		} else {
			identity.Type = "unknown"
		}
	}
	if identity.Workspace == "" {
		values := sortedKeys(workspaces)
		if len(values) == 1 {
			identity.Workspace = values[0]
		}
	}
	if len(options.Pods) > 0 {
		identity.Pods = uniqueSorted(options.Pods)
	} else {
		identity.Pods = sortedKeys(pods)
	}
	if len(identity.Pods) == 0 && s.podResolver != nil {
		resolved, resolveErr := s.podResolver(ctx, options.Workload, options.Type, options.Workspace)
		if resolveErr != nil {
			return workloadIdentity{}, fmt.Errorf("指标窗口内没有 Pod，平台 API 回退也失败: %w", resolveErr)
		}
		identity.Pods = uniqueSorted(resolved.Pods)
		if resolved.Type != "" {
			identity.Type = resolved.Type
		}
		if resolved.Workspace != "" {
			identity.Workspace = resolved.Workspace
		}
		if len(identity.Pods) > 0 {
			identity.Warnings = append(identity.Warnings, "Pod 名来自平台 API，指标窗口内无工作负载映射数据")
		}
	}
	if len(identity.Pods) == 0 {
		return workloadIdentity{}, fmt.Errorf("工作负载 %q 在查询区间内没有可解析的 Pod；可用 --pod 指定已知 Pod，或扩大 --since", options.Workload)
	}
	return identity, nil
}

func applyIdentity(options QueryOptions, identity workloadIdentity) QueryOptions {
	options.Type = identity.Type
	options.Workspace = identity.Workspace
	options.Pods = identity.Pods
	return options
}

func metricWorkloadTypeMatcher(value string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ait":
		return workloadTypeLabel, "training-job"
	case "air":
		return workloadTypeLabel + "=~", "^(?:air|infer-gateway)$"
	default:
		return workloadTypeLabel, strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeMetricWorkloadType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "training-job":
		return "ait"
	case "infer-gateway":
		return "air"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func selector(metric string, matchers map[string]string) string {
	keys := make([]string, 0, len(matchers))
	for key := range matchers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, rawKey := range keys {
		value := matchers[rawKey]
		key := rawKey
		operator := "="
		switch {
		case strings.HasSuffix(key, "!="):
			key = strings.TrimSuffix(key, "!=")
			operator = "!="
		case strings.HasSuffix(key, "!"):
			key = strings.TrimSuffix(key, "!")
			operator = "!="
		case strings.HasSuffix(key, "=~"):
			key = strings.TrimSuffix(key, "=~")
			operator = "=~"
		}
		parts = append(parts, key+operator+strconv.Quote(value))
	}
	if len(parts) == 0 {
		return metric
	}
	return metric + "{" + strings.Join(parts, ",") + "}"
}

func podMatchers(pods []string) map[string]string {
	if len(pods) == 1 {
		return map[string]string{"pod": pods[0]}
	}
	escaped := make([]string, 0, len(pods))
	for _, pod := range uniqueSorted(pods) {
		escaped = append(escaped, regexp.QuoteMeta(pod))
	}
	return map[string]string{"pod=~": "^(?:" + strings.Join(escaped, "|") + ")$"}
}

func mergeMatchers(left map[string]string, right map[string]string) map[string]string {
	result := make(map[string]string, len(left)+len(right))
	for key, value := range left {
		result[key] = value
	}
	for key, value := range right {
		result[key] = value
	}
	return result
}

func preferWorkspaceSeries(raw []RawSeries) []RawSeries {
	groups := map[string][]RawSeries{}
	for _, item := range raw {
		groups[rawIdentity(item.Metric)] = append(groups[rawIdentity(item.Metric)], item)
	}
	result := make([]RawSeries, 0, len(raw))
	for _, items := range groups {
		preferred := make([]RawSeries, 0, len(items))
		for _, item := range items {
			if metricLabel(item.Metric, "sco_srm_type") == workspaceResourceType {
				preferred = append(preferred, item)
			}
		}
		if len(preferred) > 0 {
			if dropped := len(items) - len(preferred); dropped > 0 {
				preferred[0].Metric = cloneLabels(preferred[0].Metric)
				preferred[0].Metric["__rayctl_dropped_series"] = strconv.Itoa(dropped)
				types := map[string]struct{}{}
				for _, item := range items {
					value := metricLabel(item.Metric, "sco_srm_type")
					if value != "" && value != workspaceResourceType {
						types[value] = struct{}{}
					}
				}
				preferred[0].Metric["__rayctl_dropped_types"] = strings.Join(sortedKeys(types), ",")
			}
			result = append(result, preferred...)
		} else {
			result = append(result, items...)
		}
	}
	return result
}

func mergeDuplicateRawSeries(raw []RawSeries) []RawSeries {
	type mergedSeries struct {
		labels map[string]string
		points map[int64]float64
	}
	groups := map[string]*mergedSeries{}
	for _, item := range raw {
		key := rawIdentity(item.Metric)
		group := groups[key]
		if group == nil {
			group = &mergedSeries{labels: cloneLabels(item.Metric), points: map[int64]float64{}}
			groups[key] = group
		}
		for index := 0; index < len(item.Values) && index < len(item.Timestamps); index++ {
			timestamp := item.Timestamps[index]
			value := item.Values[index]
			if old, exists := group.points[timestamp]; !exists || value > old {
				group.points[timestamp] = value
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]RawSeries, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		timestamps := sortedPointTimestamps(group.points)
		item := RawSeries{Metric: group.labels, Timestamps: timestamps, Values: make([]float64, 0, len(timestamps))}
		for _, timestamp := range timestamps {
			item.Values = append(item.Values, group.points[timestamp])
		}
		result = append(result, item)
	}
	return result
}

func containsHardwareType(series []*pointSeries, hardware string) bool {
	for _, item := range series {
		if item.hardware == hardware {
			return true
		}
	}
	return false
}

func rawIdentity(labels map[string]string) string {
	return strings.Join([]string{
		metricLabel(labels, "__name__"),
		metricLabel(labels, "pod", "pod_name"),
		metricLabel(labels, "container", "container_name"),
		metricLabel(labels, "gpu", "device", "minor_number"),
		metricLabel(labels, "uid"),
		metricLabel(labels, "reason"),
		metricLabel(labels, "extended_resource__type"),
	}, "\x00")
}

type identityFunc func(map[string]string) (string, map[string]string)

func memoryIdentity(labels map[string]string) (string, map[string]string) {
	pod := metricLabel(labels, "pod", "pod_name")
	container := metricLabel(labels, "container", "container_name")
	node := metricLabel(labels, "node", "Hostname", "hostname")
	return pod + "\x00" + container, map[string]string{"pod": pod, "container": container, "node": node}
}

func acceleratorIdentity(labels map[string]string) (string, map[string]string) {
	pod := metricLabel(labels, "pod", "pod_name")
	container := metricLabel(labels, "container", "container_name")
	gpu := metricLabel(labels, "gpu", "device", "minor_number")
	hardware := metricLabel(labels, "extended_resource__type")
	node := metricLabel(labels, "node", "Hostname", "hostname")
	return strings.Join([]string{pod, container, gpu, hardware}, "\x00"), map[string]string{"pod": pod, "container": container, "gpu": gpu, "hardware": hardware, "node": node}
}

func mergeRawSeries(raw []RawSeries, identity identityFunc, unit string) []*pointSeries {
	result := map[string]*pointSeries{}
	for _, item := range raw {
		key, labels := identity(item.Metric)
		entry := result[key]
		if entry == nil {
			entry = &pointSeries{name: displaySeriesName(labels, "gpu"), labels: labels, points: map[int64]float64{}, unit: unit, hardware: labels["hardware"]}
			result[key] = entry
		}
		entry.mergedFrom++
		if current, incoming := entry.labels["node"], labels["node"]; current != "" && incoming != "" && current != incoming {
			entry.labels["node"] = ""
		}
		if dropped, err := strconv.Atoi(item.Metric["__rayctl_dropped_series"]); err == nil {
			entry.droppedSeries += dropped
			entry.mergedFrom += dropped
		}
		if values := strings.TrimSpace(item.Metric["__rayctl_dropped_types"]); values != "" {
			entry.droppedTypes = uniqueSorted(append(entry.droppedTypes, strings.Split(values, ",")...))
		}
		for index := 0; index < len(item.Values) && index < len(item.Timestamps); index++ {
			ts := item.Timestamps[index]
			value := item.Values[index]
			if old, ok := entry.points[ts]; !ok || value > old {
				entry.points[ts] = value
			}
		}
	}
	items := make([]*pointSeries, 0, len(result))
	for _, item := range result {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	return items
}

type aggregateMode int

const (
	aggregateSum aggregateMode = iota
	aggregateMax
)

func aggregateSeries(input []*pointSeries, by string, mode aggregateMode) []*pointSeries {
	result := map[string]*pointSeries{}
	for _, source := range input {
		labels := source.labels
		groupLabels := map[string]string{"pod": labels["pod"]}
		switch by {
		case "container":
			groupLabels["container"] = labels["container"]
		case "gpu":
			groupLabels["container"] = labels["container"]
			groupLabels["gpu"] = labels["gpu"]
		}
		if labels["hardware"] != "" {
			groupLabels["hardware"] = labels["hardware"]
		}
		groupLabels["node"] = labels["node"]
		key := displaySeriesName(groupLabels, by)
		target := result[key]
		if target == nil {
			target = &pointSeries{name: key, labels: groupLabels, points: map[int64]float64{}, unit: source.unit, hardware: labels["hardware"]}
			result[key] = target
		}
		if current, incoming := target.labels["node"], labels["node"]; current != "" && incoming != "" && current != incoming {
			target.labels["node"] = ""
		}
		target.mergedFrom += source.mergedFrom
		target.droppedSeries += source.droppedSeries
		target.droppedTypes = uniqueSorted(append(target.droppedTypes, source.droppedTypes...))
		for timestamp, value := range source.points {
			current, exists := target.points[timestamp]
			if !exists || mode == aggregateSum {
				target.points[timestamp] = current + value
			} else if value > current {
				target.points[timestamp] = value
			}
		}
	}
	items := make([]*pointSeries, 0, len(result))
	for _, item := range result {
		item.points = coalesceNearbyPoints(item.points, mode, 5*time.Second)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	return items
}

func coalesceNearbyPoints(points map[int64]float64, mode aggregateMode, tolerance time.Duration) map[int64]float64 {
	if len(points) < 2 || tolerance <= 0 {
		return points
	}
	timestamps := sortedPointTimestamps(points)
	result := make(map[int64]float64, len(points))
	anchor := timestamps[0]
	value := points[anchor]
	flush := func() {
		result[anchor] = value
	}
	for _, timestamp := range timestamps[1:] {
		if timestamp-anchor > tolerance.Milliseconds() {
			flush()
			anchor = timestamp
			value = points[timestamp]
			continue
		}
		if mode == aggregateSum {
			value += points[timestamp]
		} else if points[timestamp] > value {
			value = points[timestamp]
		}
	}
	flush()
	return result
}

func applyLimits(series []*pointSeries, limits []*pointSeries) {
	byName := map[string]*pointSeries{}
	for _, item := range limits {
		byName[item.name] = item
	}
	for _, item := range series {
		limit := byName[item.name]
		if limit == nil {
			continue
		}
		for _, value := range limit.points {
			if value > item.limit {
				item.limit = value
			}
		}
	}
}

func buildResult(command string, options QueryOptions, identity workloadIdentity, input []*pointSeries) *Result {
	mode := "raw_samples"
	if options.UseStep {
		mode = "step"
	}
	result := &Result{Metadata: Metadata{
		Command: command, Workload: options.Workload, WorkloadType: identity.Type, Workspace: identity.Workspace, Environment: options.Environment,
		Pods: identity.Pods, Start: formatTime(options.Start), End: formatTime(options.End), Range: TimeRange{Start: formatTime(options.Start), End: formatTime(options.End)}, Timezone: "UTC+8", Mode: mode,
	}, Series: []Series{}, Samples: []Sample{}, Warnings: []string{}}
	if options.UseStep {
		result.Metadata.Step = stringPointer(options.Step.String())
	}
	result.Metadata.Metric = map[string]string{"mem": memoryMetric, "gpu": acceleratorMetric}[command]
	result.Metadata.Unit = map[string]string{"mem": "bytes", "gpu": "percent"}[command]
	for _, item := range input {
		series := Series{
			Name: item.name, Unit: item.unit, Limit: item.limit,
			MergedFrom: item.mergedFrom, DroppedSeries: item.droppedSeries, DroppedSCOSRMTypes: append([]string{}, item.droppedTypes...), Hardware: item.hardware,
			Pod: optionalString(item.labels["pod"]), Container: optionalString(item.labels["container"]),
			Node: optionalString(item.labels["node"]), GPU: optionalString(item.labels["gpu"]),
		}
		timestamps := sortedPointTimestamps(item.points)
		for _, timestamp := range timestamps {
			value := item.points[timestamp]
			sample := Sample{Series: item.name, Timestamp: formatTimestamp(timestamp), Value: value}
			if item.limit > 0 {
				sample.Percent = value / item.limit * 100
			} else if command == "gpu" {
				sample.Percent = value
			}
			result.Samples = append(result.Samples, sample)
			updateSeriesSummary(&series.Summary, sample)
		}
		if item.limit > 0 {
			series.Summary.PeakPercent = series.Summary.Peak / item.limit * 100
		} else if command == "gpu" {
			series.Summary.PeakPercent = series.Summary.Peak
		}
		result.Series = append(result.Series, series)
	}
	sortSamples(result.Samples)
	result.Summary.SeriesCount = len(result.Series)
	result.Summary.SampleCount = len(result.Samples)
	result.Metadata.SampleInterval = medianSampleInterval(result.Samples)
	result.Metadata.SampleIntervalSeconds = durationSeconds(result.Metadata.SampleInterval)
	appendSamplingWarning(result)
	return result
}

func buildRestartResult(options QueryOptions, identity workloadIdentity, restartRaw []RawSeries, reasonRaw []RawSeries) *Result {
	result := &Result{Metadata: Metadata{
		Command: "restart", Workload: options.Workload, WorkloadType: identity.Type, Workspace: identity.Workspace, Environment: options.Environment,
		Pods: identity.Pods, Start: formatTime(options.Start), End: formatTime(options.End), Range: TimeRange{Start: formatTime(options.Start), End: formatTime(options.End)}, Timezone: "UTC+8", Metric: restartMetric, Unit: "count",
		Mode: map[bool]string{true: "step", false: "raw_samples"}[options.UseStep],
	}, Series: []Series{}, Samples: []Sample{}, Warnings: []string{}}
	if options.UseStep {
		result.Metadata.Step = stringPointer(options.Step.String())
	}
	reasons := reasonSamples(reasonRaw)
	groups := map[string][]RawSeries{}
	for _, item := range restartRaw {
		pod := metricLabel(item.Metric, "pod", "pod_name")
		container := metricLabel(item.Metric, "container", "container_name")
		groups[pod+"\x00"+container] = append(groups[pod+"\x00"+container], item)
	}
	for key, rows := range groups {
		parts := strings.Split(key, "\x00")
		name := parts[0]
		if len(parts) > 1 && parts[1] != "" {
			name += "/" + parts[1]
		}
		series := Series{Name: name, Unit: "count", MergedFrom: len(rows),
			Pod: optionalString(parts[0]), Container: optionalString(parts[1]), DroppedSCOSRMTypes: []string{},
		}
		segments := make([]RawSeries, len(rows))
		copy(segments, rows)
		sort.Slice(segments, func(i, j int) bool { return firstTimestamp(segments[i]) < firstTimestamp(segments[j]) })
		for segmentIndex, row := range segments {
			uid := metricLabel(row.Metric, "uid")
			pairs := sortedRawPairs(row)
			var previous float64
			havePrevious := false
			for _, pair := range pairs {
				if pair.timestamp < options.Start.UnixMilli() {
					previous = pair.value
					havePrevious = true
				}
			}
			if havePrevious {
				baseline := Sample{Series: name, Timestamp: formatTime(options.Start), Value: previous, UID: uid, Event: "baseline"}
				result.Samples = append(result.Samples, baseline)
				updateSeriesSummary(&series.Summary, baseline)
			}
			for _, pair := range pairs {
				if pair.timestamp < options.Start.UnixMilli() {
					continue
				}
				if !havePrevious {
					previous = pair.value
					havePrevious = true
					event := "baseline"
					if segmentIndex > 0 {
						event = "series_reset"
						result.Warnings = append(result.Warnings, fmt.Sprintf("%s 在 %s 检测到新 Pod UID %s，计数器已重建", name, formatTimestamp(pair.timestamp), emptyValue(uid)))
					}
					sample := Sample{Series: name, Timestamp: formatTimestamp(pair.timestamp), Value: pair.value, UID: uid, Event: event}
					result.Samples = append(result.Samples, sample)
					updateSeriesSummary(&series.Summary, sample)
					continue
				}
				delta := pair.value - previous
				if delta < 0 {
					result.Warnings = append(result.Warnings, fmt.Sprintf("%s 在 %s 的重启计数器发生回退，按新序列处理", name, formatTimestamp(pair.timestamp)))
					sample := Sample{Series: name, Timestamp: formatTimestamp(pair.timestamp), Value: pair.value, UID: uid, Event: "series_reset"}
					result.Samples = append(result.Samples, sample)
					updateSeriesSummary(&series.Summary, sample)
					previous = pair.value
					continue
				}
				if delta > 0 {
					reason := nearestReason(reasons, parts[0], parts[1], pair.timestamp)
					result.Samples = append(result.Samples, Sample{
						Series: name, Timestamp: formatTimestamp(pair.timestamp), Value: pair.value, Delta: delta,
						Reason: reason, UID: uid, Event: "restart",
					})
					updateSeriesSummary(&series.Summary, result.Samples[len(result.Samples)-1])
					if reason == "" {
						result.Warnings = append(result.Warnings, fmt.Sprintf("%s 在 %s 的终止原因未采集到", name, formatTimestamp(pair.timestamp)))
					}
					if delta > 1 {
						result.Warnings = append(result.Warnings, fmt.Sprintf("%s 在一个采样间隔内增加 %.0f 次重启，只能匹配到最后一次终止原因", name, delta))
					}
				}
				previous = pair.value
			}
		}
		result.Series = append(result.Series, series)
	}
	sort.Slice(result.Series, func(i, j int) bool { return result.Series[i].Name < result.Series[j].Name })
	sortSamples(result.Samples)
	result.Summary.SeriesCount = len(result.Series)
	result.Summary.SampleCount = len(result.Samples)
	for _, sample := range result.Samples {
		if sample.Event == "restart" {
			result.Summary.EventCount += int(math.Max(1, sample.Delta))
		}
	}
	result.Metadata.SampleInterval = medianRawInterval(restartRaw)
	result.Metadata.SampleIntervalSeconds = durationSeconds(result.Metadata.SampleInterval)
	appendSamplingWarning(result)
	return result
}

type reasonPoint struct {
	pod       string
	container string
	reason    string
	timestamp int64
}

func reasonSamples(raw []RawSeries) []reasonPoint {
	result := make([]reasonPoint, 0)
	for _, item := range raw {
		for index := 0; index < len(item.Values) && index < len(item.Timestamps); index++ {
			if item.Values[index] <= 0 {
				continue
			}
			result = append(result, reasonPoint{
				pod: metricLabel(item.Metric, "pod", "pod_name"), container: metricLabel(item.Metric, "container", "container_name"),
				reason: metricLabel(item.Metric, "reason"), timestamp: item.Timestamps[index],
			})
		}
	}
	return result
}

func nearestReason(points []reasonPoint, pod string, container string, timestamp int64) string {
	best := ""
	bestDistance := int64(5 * time.Minute / time.Millisecond)
	for _, point := range points {
		if point.pod != pod || point.container != container {
			continue
		}
		distance := point.timestamp - timestamp
		if distance < 0 {
			distance = -distance
		}
		if distance <= bestDistance {
			bestDistance = distance
			best = point.reason
		}
	}
	return best
}

func appendAcceleratorMemorySummary(result *Result, used []*pointSeries, total []*pointSeries) {
	totalByName := map[string]*pointSeries{}
	for _, item := range total {
		totalByName[item.name] = item
	}
	seriesByName := map[string]*Series{}
	for index := range result.Series {
		seriesByName[result.Series[index].Name] = &result.Series[index]
	}
	for _, item := range used {
		capacity := totalByName[item.name]
		var usedPeak float64
		for _, value := range item.points {
			usedPeak = math.Max(usedPeak, value)
		}
		var totalPeak float64
		if capacity != nil {
			for _, value := range capacity.points {
				totalPeak = math.Max(totalPeak, value)
			}
		}
		if target := seriesByName[item.name]; target != nil {
			target.MemoryUsedPeakMiB = usedPeak
			target.MemoryTotalMiB = totalPeak
		}
	}
}

func metricLabel(labels map[string]string, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(labels[name]); value != "" {
			return value
		}
	}
	return ""
}

func displaySeriesName(labels map[string]string, by string) string {
	parts := []string{emptyValue(labels["pod"])}
	if by == "container" || by == "gpu" {
		if labels["container"] != "" {
			parts = append(parts, labels["container"])
		}
	}
	if by == "gpu" && labels["gpu"] != "" {
		parts = append(parts, "gpu="+labels["gpu"])
	}
	return strings.Join(parts, "/")
}

func rawSeriesName(labels map[string]string, index int) string {
	if name := metricLabel(labels, "__name__"); name != "" {
		meaningful := []string{}
		for _, key := range []string{"pod", "container", "gpu", "instance"} {
			if value := metricLabel(labels, key); value != "" {
				meaningful = append(meaningful, key+"="+value)
			}
		}
		if len(meaningful) > 0 {
			return name + "{" + strings.Join(meaningful, ",") + "}"
		}
		return name
	}
	return fmt.Sprintf("series-%d", index+1)
}

func updateSeriesSummary(summary *SeriesSummary, sample Sample) {
	if summary == nil {
		return
	}
	if summary.SampleCount == 0 || sample.Value > summary.Peak {
		summary.Peak = sample.Value
		summary.PeakAt = sample.Timestamp
	}
	summary.Latest = sample.Value
	summary.SampleCount++
}

func appendSamplingWarning(result *Result) {
	if result == nil || result.Metadata.SampleIntervalSeconds <= 0 {
		return
	}
	warning := fmt.Sprintf("采样间隔约 %s，间隔内发生的事件无法被观测", result.Metadata.SampleInterval)
	for _, existing := range result.Warnings {
		if existing == warning {
			return
		}
	}
	result.Warnings = append(result.Warnings, warning)

}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringPointer(value string) *string {
	return &value
}

func sortedPointTimestamps(points map[int64]float64) []int64 {
	result := make([]int64, 0, len(points))
	for timestamp := range points {
		result = append(result, timestamp)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

type rawPair struct {
	timestamp int64
	value     float64
}

func sortedRawPairs(raw RawSeries) []rawPair {
	result := make([]rawPair, 0, len(raw.Values))
	for index := 0; index < len(raw.Values) && index < len(raw.Timestamps); index++ {
		result = append(result, rawPair{timestamp: raw.Timestamps[index], value: raw.Values[index]})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].timestamp < result[j].timestamp })
	return result
}

func firstTimestamp(raw RawSeries) int64 {
	if len(raw.Timestamps) == 0 {
		return math.MaxInt64
	}
	result := raw.Timestamps[0]
	for _, timestamp := range raw.Timestamps[1:] {
		if timestamp < result {
			result = timestamp
		}
	}
	return result
}

func medianRawInterval(raw []RawSeries) string {
	intervals := make([]int64, 0)
	for _, item := range raw {
		values := append([]int64(nil), item.Timestamps...)
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		for index := 1; index < len(values); index++ {
			if delta := values[index] - values[index-1]; delta > 0 {
				intervals = append(intervals, delta)
			}
		}
	}
	return formatMedianInterval(intervals)
}

func medianSampleInterval(samples []Sample) string {
	bySeries := map[string][]int64{}
	for _, sample := range samples {
		value, err := time.Parse(time.RFC3339Nano, sample.Timestamp)
		if err == nil {
			bySeries[sample.Series] = append(bySeries[sample.Series], value.UnixMilli())
		}
	}
	intervals := make([]int64, 0)
	for _, values := range bySeries {
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		for index := 1; index < len(values); index++ {
			if delta := values[index] - values[index-1]; delta > 0 {
				intervals = append(intervals, delta)
			}
		}
	}
	return formatMedianInterval(intervals)
}

func formatMedianInterval(intervals []int64) string {
	if len(intervals) == 0 {
		return ""
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i] < intervals[j] })
	value := intervals[len(intervals)/2]
	return (time.Duration(value) * time.Millisecond).Round(time.Second).String()
}

func durationSeconds(value string) float64 {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return duration.Seconds()
}

func formatTime(value time.Time) string {
	return value.In(BeijingLocation).Format(time.RFC3339)
}

func formatTimestamp(milliseconds int64) string {
	return formatTime(time.UnixMilli(milliseconds))
}

func sortSamples(samples []Sample) {
	sort.SliceStable(samples, func(i, j int) bool {
		if samples[i].Timestamp == samples[j].Timestamp {
			return samples[i].Series < samples[j].Series
		}
		return samples[i].Timestamp < samples[j].Timestamp
	})
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	return sortedKeys(set)
}

func cloneLabels(labels map[string]string) map[string]string {
	result := make(map[string]string, len(labels))
	for key, value := range labels {
		result[key] = value
	}
	return result
}

func emptyValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
