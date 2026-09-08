package nodehealth

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const mindxDeviceInfoStaleAfter = 10 * time.Minute

type mindxNodeInfoPayload struct {
	NodeInfo struct {
		FaultDevList []map[string]any `json:"FaultDevList"`
		NodeStatus   string           `json:"NodeStatus"`
	} `json:"NodeInfo"`
}

type mindxDeviceInfoPayload struct {
	DeviceInfo struct {
		DeviceList map[string]any `json:"DeviceList"`
		UpdateTime any            `json:"UpdateTime"`
	} `json:"DeviceInfo"`
}

func (s *Service) checkMindXNPU(ctx context.Context, node *corev1.Node, now time.Time) (CheckResult, error) {
	if s.environment != "d" {
		return skipCheck(fmt.Sprintf("mindx-npu 仅适用于 D 环境，当前环境=%s", emptyValue(s.environment))), nil
	}
	nodeInfoName := "mindx-dl-nodeinfo-" + node.Name
	deviceInfoName := "mindx-dl-deviceinfo-" + node.Name
	type cmResult struct {
		cm  *corev1.ConfigMap
		err error
	}
	var nodeInfoResult cmResult
	var deviceInfoResult cmResult
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		nodeInfoResult.cm, nodeInfoResult.err = s.client.CoreV1().ConfigMaps("mindx-dl").Get(ctx, nodeInfoName, metav1.GetOptions{})
	}()
	go func() {
		defer wait.Done()
		deviceInfoResult.cm, deviceInfoResult.err = s.client.CoreV1().ConfigMaps("kube-system").Get(ctx, deviceInfoName, metav1.GetOptions{})
	}()
	wait.Wait()
	if isNotFound(nodeInfoResult.err) {
		nodeInfoResult.cm = nil
		nodeInfoResult.err = nil
	}
	if isNotFound(deviceInfoResult.err) {
		deviceInfoResult.cm = nil
		deviceInfoResult.err = nil
	}
	if nodeInfoResult.err != nil && !isNotFound(nodeInfoResult.err) {
		return CheckResult{}, fmt.Errorf("get %s: %w", nodeInfoName, nodeInfoResult.err)
	}
	if deviceInfoResult.err != nil && !isNotFound(deviceInfoResult.err) {
		return CheckResult{}, fmt.Errorf("get %s: %w", deviceInfoName, deviceInfoResult.err)
	}

	evidence := map[string]any{
		"nodeinfo_exists":    nodeInfoResult.cm != nil,
		"deviceinfo_exists":  deviceInfoResult.cm != nil,
		"node_ready":         nodeReady(node),
		"node_taints":        len(node.Spec.Taints),
		"node_unschedulable": node.Spec.Unschedulable,
	}
	deviceHealthy := false
	deviceStale := false
	if deviceInfoResult.cm != nil {
		var payload mindxDeviceInfoPayload
		if err := json.Unmarshal([]byte(deviceInfoResult.cm.Data["DeviceInfoCfg"]), &payload); err != nil {
			return CheckResult{}, fmt.Errorf("parse %s DeviceInfoCfg: %w", deviceInfoName, err)
		}
		fault := stringValue(payload.DeviceInfo.DeviceList["huawei.com/Ascend910-Fault"])
		network := stringValue(payload.DeviceInfo.DeviceList["huawei.com/Ascend910-NetworkUnhealthy"])
		updateTime := int64Value(payload.DeviceInfo.UpdateTime)
		ageSeconds := int64(0)
		if updateTime > 0 {
			ageSeconds = maxInt64(0, now.Unix()-updateTime)
			deviceStale = ageSeconds > int64(mindxDeviceInfoStaleAfter.Seconds())
		}
		deviceHealthy = isEmptyDeviceList(fault) && isEmptyDeviceList(network)
		evidence["device_fault"] = fault
		evidence["device_network_unhealthy"] = network
		evidence["device_update_time"] = updateTime
		evidence["device_age_seconds"] = ageSeconds
		evidence["device_healthy"] = deviceHealthy
	}

	if nodeInfoResult.cm == nil {
		status := StatusOK
		message := "nodeinfo 不存在（正常按需状态）"
		if deviceStale {
			status = StatusWarn
			message += fmt.Sprintf("，deviceinfo 已 %ds 未更新", evidence["device_age_seconds"])
		}
		return CheckResult{Status: status, Message: message, Evidence: evidence}, nil
	}

	var nodePayload mindxNodeInfoPayload
	if err := json.Unmarshal([]byte(nodeInfoResult.cm.Data["NodeInfo"]), &nodePayload); err != nil {
		return CheckResult{}, fmt.Errorf("parse %s NodeInfo: %w", nodeInfoName, err)
	}
	nodeStatus := strings.TrimSpace(nodePayload.NodeInfo.NodeStatus)
	evidence["nodeinfo_status"] = nodeStatus
	evidence["nodeinfo_fault_devices"] = nodePayload.NodeInfo.FaultDevList
	if strings.EqualFold(nodeStatus, "Healthy") {
		status := StatusOK
		message := "nodeinfo=Healthy"
		if deviceStale {
			status = StatusWarn
			message += fmt.Sprintf("，deviceinfo 已 %ds 未更新", evidence["device_age_seconds"])
		}
		return CheckResult{Status: status, Message: message, Evidence: evidence}, nil
	}

	nodeHealthy := nodeReady(node) && len(node.Spec.Taints) == 0 && !node.Spec.Unschedulable
	if nodeHealthy && deviceInfoResult.cm != nil && deviceHealthy {
		return CheckResult{
			Status:   StatusFail,
			Message:  fmt.Sprintf("nodeinfo=%s，但 node Ready 且 deviceinfo 无故障", emptyValue(nodeStatus)),
			Evidence: evidence,
		}, nil
	}
	message := fmt.Sprintf("nodeinfo=%s，node_healthy=%t，device_healthy=%t", emptyValue(nodeStatus), nodeHealthy, deviceHealthy)
	return CheckResult{Status: StatusWarn, Message: message, Evidence: evidence}, nil
}

func isEmptyDeviceList(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "[]" || value == "null"
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	case string:
		result, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return result
	default:
		return 0
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func emptyValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}
