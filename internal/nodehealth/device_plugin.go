package nodehealth

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"rayctl/internal/nodeexec"
)

const (
	pluginNamespace = "kube-system"
	pluginContainer = "rdma-device-plugin"
	resourceHCA     = "rdma/hca"
	resourceRoCE    = "rdma-training/roce"
)

var (
	pluginRoundPattern    = regexp.MustCompile(`(?i)discovering host network devices`)
	pluginResourcePattern = regexp.MustCompile(`(?i)(?:no changes to devices for|updating)\s+"([^"]+)"(?:\s+devices)?`)
	pluginExposePattern   = regexp.MustCompile(`(?i)exposing\s+"([0-9]+)"\s+devices`)
)

type deviceResourceEvidence struct {
	Resource        string `json:"resource"`
	Exposing        *int   `json:"exposing"`
	Checkpoint      *int   `json:"checkpoint"`
	Capacity        int64  `json:"capacity"`
	CapacityPresent bool   `json:"capacity_present"`
	IBPorts         *int   `json:"ib_ports,omitempty"`
	Verdict         Status `json:"verdict"`
	Reason          string `json:"reason,omitempty"`
}

func (s *Service) checkDevicePlugin(ctx context.Context, node *corev1.Node) (CheckResult, error) {
	pod, phase, err := s.findRDMAPluginPod(ctx, node.Name)
	if err != nil {
		return CheckResult{}, err
	}
	if pod == "" {
		return skipCheck("节点上没有 rdma-device-plugin Pod"), nil
	}
	if phase != corev1.PodRunning {
		return CheckResult{
			Status:  StatusFail,
			Message: fmt.Sprintf("插件 Pod %s phase=%s", pod, phase),
			Evidence: map[string]any{
				"pod": pod, "phase": phase,
			},
		}, nil
	}
	if s.executor == nil {
		return CheckResult{}, fmt.Errorf("node exec is unavailable")
	}

	exposing, err := s.latestPluginExposing(ctx, pod)
	if err != nil {
		return CheckResult{}, err
	}
	if len(exposing) == 0 {
		return skipCheck("无法获取插件最近一轮基准"), nil
	}
	checkpointRaw, err := s.executor.Run(ctx, node.Name, nodeexec.OperationCheckpoint)
	if err != nil {
		return CheckResult{}, err
	}
	registered, err := parseRegisteredDevices(checkpointRaw)
	if err != nil {
		return CheckResult{}, fmt.Errorf("parse kubelet checkpoint: %w", err)
	}
	interfacesRaw, err := s.executor.Run(ctx, node.Name, nodeexec.OperationIBInterfaces)
	if err != nil {
		return CheckResult{}, err
	}
	ibPorts := countIBInterfaces(interfacesRaw)

	resources := make([]deviceResourceEvidence, 0, 2)
	status := StatusSkip
	parts := make([]string, 0, 2)
	for _, name := range []string{resourceHCA, resourceRoCE} {
		exposingCount, hasBaseline := exposing[name]
		if !hasBaseline {
			resources = append(resources, deviceResourceEvidence{
				Resource: name, Verdict: StatusSkip, Reason: "最近一轮日志无该资源基准",
			})
			parts = append(parts, fmt.Sprintf("%s baseline=missing (SKIP)", name))
			continue
		}
		checkpointCount, checkpointPresent := registered[name]
		capacityQuantity, capacityPresent := node.Status.Capacity[corev1.ResourceName(name)]
		capacity := capacityQuantity.Value()
		resource := evaluateDeviceResource(name, exposingCount, checkpointCount, checkpointPresent, capacity, capacityPresent, ibPorts)
		resources = append(resources, resource)
		status = WorstStatus(status, resource.Verdict)
		checkpointText := "missing"
		if resource.Checkpoint != nil {
			checkpointText = strconv.Itoa(*resource.Checkpoint)
		}
		part := fmt.Sprintf("%s exposing=%d checkpoint=%s capacity=%d", name, exposingCount, checkpointText, capacity)
		if resource.IBPorts != nil {
			part += fmt.Sprintf(" ib=%d", *resource.IBPorts)
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", part, resource.Verdict))
	}

	return CheckResult{
		Status:  status,
		Message: strings.Join(parts, "；"),
		Evidence: map[string]any{
			"pod":                pod,
			"phase":              phase,
			"resources":          resources,
			"registered_devices": registered,
		},
	}, nil
}

func (s *Service) findRDMAPluginPod(ctx context.Context, node string) (string, corev1.PodPhase, error) {
	pods, err := s.client.CoreV1().Pods(pluginNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "name=rdma-device-plugin",
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node).String(),
	})
	if err != nil {
		return "", "", fmt.Errorf("list rdma device plugin pods on %s: %w", node, err)
	}
	if len(pods.Items) == 0 {
		return "", "", nil
	}
	sort.SliceStable(pods.Items, func(i, j int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[j].CreationTimestamp.Time)
	})
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Name, pod.Status.Phase, nil
		}
	}
	return pods.Items[0].Name, pods.Items[0].Status.Phase, nil
}

func (s *Service) latestPluginExposing(ctx context.Context, pod string) (map[string]int, error) {
	for _, tail := range []int64{40, 200} {
		logs, err := s.client.CoreV1().Pods(pluginNamespace).GetLogs(pod, &corev1.PodLogOptions{
			Container: pluginContainer,
			TailLines: &tail,
		}).DoRaw(ctx)
		if err != nil {
			return nil, fmt.Errorf("read rdma device plugin logs from %s: %w", pod, err)
		}
		result := parseLatestPluginRound(string(logs))
		if _, hca := result[resourceHCA]; hca {
			if _, roce := result[resourceRoCE]; roce {
				return result, nil
			}
		}
		if tail == 200 {
			return result, nil
		}
	}
	return nil, nil
}

func parseLatestPluginRound(logs string) map[string]int {
	lines := strings.Split(logs, "\n")
	start := -1
	for index := len(lines) - 1; index >= 0; index-- {
		if pluginRoundPattern.MatchString(lines[index]) {
			start = index
			break
		}
	}
	if start < 0 {
		return nil
	}
	result := make(map[string]int)
	resourceName := ""
	for _, line := range lines[start:] {
		if matches := pluginResourcePattern.FindStringSubmatch(line); len(matches) == 2 {
			resourceName = strings.TrimSpace(matches[1])
			continue
		}
		matches := pluginExposePattern.FindStringSubmatch(line)
		if len(matches) != 2 || resourceName == "" {
			continue
		}
		value, err := strconv.Atoi(matches[1])
		if err == nil {
			result[resourceName] = value
		}
		resourceName = ""
	}
	return result
}

func parseRegisteredDevices(payload string) (map[string]int, error) {
	var root any
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &root); err != nil {
		return nil, err
	}
	registered, ok := findObjectKey(root, "RegisteredDevices")
	if !ok {
		return nil, fmt.Errorf("RegisteredDevices field is missing")
	}
	object, ok := registered.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("RegisteredDevices is not an object")
	}
	result := make(map[string]int, len(object))
	for name, raw := range object {
		result[name] = registeredDeviceCount(raw)
	}
	return result, nil
}

func findObjectKey(value any, key string) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for candidate, nested := range typed {
			if strings.EqualFold(candidate, key) {
				return nested, true
			}
		}
		for _, nested := range typed {
			if result, ok := findObjectKey(nested, key); ok {
				return result, true
			}
		}
	case []any:
		for _, nested := range typed {
			if result, ok := findObjectKey(nested, key); ok {
				return result, true
			}
		}
	}
	return nil, false
}

func registeredDeviceCount(value any) int {
	switch typed := value.(type) {
	case []any:
		return len(typed)
	case map[string]any:
		return len(typed)
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0
		}
		return len(strings.FieldsFunc(typed, func(r rune) bool { return r == ',' || r == ' ' }))
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func countIBInterfaces(output string) int {
	count := 0
	for _, value := range strings.Fields(output) {
		if strings.HasPrefix(strings.ToLower(value), "ib") {
			count++
		}
	}
	return count
}

func evaluateDeviceResource(name string, exposing int, checkpoint int, checkpointPresent bool, capacity int64, capacityPresent bool, ibPorts int) deviceResourceEvidence {
	exposingCopy := exposing
	result := deviceResourceEvidence{
		Resource: name, Exposing: &exposingCopy, Capacity: capacity, CapacityPresent: capacityPresent, Verdict: StatusOK,
	}
	if checkpointPresent {
		checkpointCopy := checkpoint
		result.Checkpoint = &checkpointCopy
	}
	if name == resourceHCA {
		ibCopy := ibPorts
		result.IBPorts = &ibCopy
	}

	switch {
	case exposing > 0 && !checkpointPresent && capacity == 0:
		result.Verdict = StatusFail
		result.Reason = "插件发现设备，但 checkpoint 无键且 capacity 为 0"
	case checkpointPresent && int64(checkpoint) != capacity:
		result.Verdict = StatusWarn
		result.Reason = "checkpoint 与 capacity 数量不一致"
	case checkpointPresent && checkpoint != exposing:
		result.Verdict = StatusWarn
		result.Reason = "插件 exposing 与 checkpoint 数量不一致"
	case !checkpointPresent && exposing != int(capacity):
		result.Verdict = StatusWarn
		result.Reason = "插件 exposing 与 capacity 数量不一致"
	}
	if name == resourceHCA && ((ibPorts > 0 && exposing == 0) || (ibPorts == 0 && exposing > 0)) {
		result.Verdict = WorstStatus(result.Verdict, StatusWarn)
		if ibPorts > 0 {
			result.Reason = "存在 IB 接口但插件 exposing 为 0"
		} else {
			result.Reason = "插件 exposing 大于 0 但未发现 IB 接口"
		}
	}
	return result
}
