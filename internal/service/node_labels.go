package service

import "strings"

func nodeAcceleratorModel(labels map[string]string) string {
	for _, key := range []string{
		"accelerator-type",
		"node.kubernetes.io/npu.chip.name",
		"metax-tech.com/gpu.product",
		"nvidia.com/gpu.product",
		"resource.compute.sensecore.cn/accelerator-model",
		"accelerator",
		"hygon.com/dcu.product",
	} {
		if value := strings.TrimSpace(labels[key]); value != "" {
			return value
		}
	}
	for key, value := range labels {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		deviceKey := strings.Contains(key, "gpu") || strings.Contains(key, "npu") || strings.Contains(key, "accelerator")
		modelKey := strings.Contains(key, "product") || strings.Contains(key, "model") || strings.Contains(key, "chip")
		if deviceKey && modelKey {
			return value
		}
	}
	return ""
}

func fillNodeModelsByMachineType(items []VCNodeListItem, modelByMachineType map[string]string) {
	candidates := make(map[string]map[string]struct{})
	for _, item := range items {
		machineType := strings.ToLower(strings.TrimSpace(item.MachineType))
		model := strings.TrimSpace(item.Model)
		if machineType == "" || model == "" {
			continue
		}
		if candidates[machineType] == nil {
			candidates[machineType] = make(map[string]struct{})
		}
		candidates[machineType][model] = struct{}{}
	}
	for machineType, models := range candidates {
		if len(models) != 1 || strings.TrimSpace(modelByMachineType[machineType]) != "" {
			continue
		}
		for model := range models {
			modelByMachineType[machineType] = model
		}
	}
	for index := range items {
		if strings.TrimSpace(items[index].Model) != "" {
			continue
		}
		items[index].Model = strings.TrimSpace(modelByMachineType[strings.ToLower(strings.TrimSpace(items[index].MachineType))])
	}
}

func missingNodeMachineTypes(items []VCNodeListItem) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range items {
		if strings.TrimSpace(item.Model) != "" {
			continue
		}
		if machineType := strings.ToLower(strings.TrimSpace(item.MachineType)); machineType != "" {
			result[machineType] = struct{}{}
		}
	}
	return result
}
