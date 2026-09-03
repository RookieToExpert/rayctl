package service

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type JobResourceSpecItem struct {
	Task                string
	Replicas            int
	CPU                 string
	Memory              string
	Accelerator         string
	AcceleratorResource string
	Model               string
	MachineType         string
}

func jobResourceSpecsFromVolcanoJob(job *unstructured.Unstructured) []JobResourceSpecItem {
	if job == nil {
		return nil
	}
	tasks, found, err := unstructured.NestedSlice(job.Object, "spec", "tasks")
	if err != nil || !found {
		return nil
	}
	result := make([]JobResourceSpecItem, 0, len(tasks))
	for _, rawTask := range tasks {
		task, ok := rawTask.(map[string]any)
		if !ok {
			continue
		}
		item := JobResourceSpecItem{
			Task:     nestedString(task, "name"),
			Replicas: nestedInt(task, "replicas"),
		}
		podSpec, found, err := unstructured.NestedMap(task, "template", "spec")
		if err != nil || !found {
			continue
		}
		item.CPU, item.Memory, item.Accelerator, item.AcceleratorResource = summarizeContainerRequests(podSpec)
		selectors := nestedStringMap(podSpec, "nodeSelector")
		item.Model = nodeAcceleratorModel(selectors)
		item.MachineType = firstNonEmpty(
			strings.TrimSpace(selectors[sspMachineTypeLabel]),
			machineTypeFromNodeAffinity(podSpec),
		)
		if item.CPU != "" || item.Memory != "" || item.Accelerator != "" || item.Model != "" || item.MachineType != "" {
			result = append(result, item)
		}
	}
	return result
}

func summarizeContainerRequests(podSpec map[string]any) (string, string, string, string) {
	containers, found, err := unstructured.NestedSlice(podSpec, "containers")
	if err != nil || !found {
		return "", "", "", ""
	}
	cpu := resource.MustParse("0")
	memory := resource.MustParse("0")
	accelerators := make(map[string]resource.Quantity)
	for _, rawContainer := range containers {
		container, ok := rawContainer.(map[string]any)
		if !ok {
			continue
		}
		resources, _, _ := unstructured.NestedMap(container, "resources")
		values, _, _ := unstructured.NestedMap(resources, "requests")
		if len(values) == 0 {
			values, _, _ = unstructured.NestedMap(resources, "limits")
		}
		for name, rawValue := range values {
			quantity, err := resource.ParseQuantity(strings.TrimSpace(fmt.Sprint(rawValue)))
			if err != nil {
				continue
			}
			switch strings.ToLower(name) {
			case "cpu":
				cpu.Add(quantity)
			case "memory":
				memory.Add(quantity)
			default:
				if isAcceleratorResourceName(name) {
					current := accelerators[name]
					current.Add(quantity)
					accelerators[name] = current
				}
			}
		}
	}
	acceleratorNames := make([]string, 0, len(accelerators))
	for name := range accelerators {
		acceleratorNames = append(acceleratorNames, name)
	}
	sort.Strings(acceleratorNames)
	acceleratorValues := make([]string, 0, len(acceleratorNames))
	for _, name := range acceleratorNames {
		quantity := accelerators[name]
		acceleratorValues = append(acceleratorValues, quantity.String())
	}
	return nonZeroQuantity(cpu), nonZeroQuantity(memory), strings.Join(acceleratorValues, "+"), strings.Join(acceleratorNames, "+")
}

func nonZeroQuantity(value resource.Quantity) string {
	if value.IsZero() {
		return ""
	}
	return value.String()
}

func isAcceleratorResourceName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, marker := range []string{"gpu", "npu", "ascend", "dcu", "mlu", "gcu", "accelerator"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func machineTypeFromNodeAffinity(podSpec map[string]any) string {
	terms, found, err := unstructured.NestedSlice(
		podSpec, "affinity", "nodeAffinity", "requiredDuringSchedulingIgnoredDuringExecution", "nodeSelectorTerms",
	)
	if err != nil || !found {
		return ""
	}
	for _, rawTerm := range terms {
		term, ok := rawTerm.(map[string]any)
		if !ok {
			continue
		}
		expressions, _, _ := unstructured.NestedSlice(term, "matchExpressions")
		for _, rawExpression := range expressions {
			expression, ok := rawExpression.(map[string]any)
			if !ok || !strings.EqualFold(nestedString(expression, "key"), sspMachineTypeLabel) {
				continue
			}
			values, _, _ := unstructured.NestedStringSlice(expression, "values")
			return strings.Join(values, ", ")
		}
	}
	return ""
}

func nestedString(object map[string]any, fields ...string) string {
	value, _, _ := unstructured.NestedString(object, fields...)
	return strings.TrimSpace(value)
}

func nestedInt(object map[string]any, fields ...string) int {
	value, found, _ := unstructured.NestedInt64(object, fields...)
	if found {
		return int(value)
	}
	raw, found, _ := unstructured.NestedFieldNoCopy(object, fields...)
	if !found {
		return 0
	}
	switch value := raw.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func nestedStringMap(object map[string]any, fields ...string) map[string]string {
	values, found, _ := unstructured.NestedStringMap(object, fields...)
	if !found {
		return nil
	}
	return values
}
