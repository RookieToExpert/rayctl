package service

import (
	"rayctl/internal/platform"
	"testing"
)

func TestAcceleratorFreeFilterAlsoRequiresCPUAndMemory(t *testing.T) {
	for _, kind := range []string{"queue", "workspace", "vc"} {
		t.Run(kind, func(t *testing.T) {
			items := []SSPQueueNodeUsageItem{
				{HostName: "free", AcceleratorTotal: 8, AcceleratorFree: 1, CPUTotal: "16", CPUAllocated: "8", MemoryTotal: "32GiB", MemoryAllocated: "16GiB"},
				{HostName: "cpu-full", AcceleratorTotal: 8, AcceleratorFree: 1, CPUTotal: "16", CPUAllocated: "16", MemoryTotal: "32GiB", MemoryAllocated: "16GiB"},
				{HostName: "memory-full", AcceleratorTotal: 8, AcceleratorFree: 1, CPUTotal: "16", CPUAllocated: "8", MemoryTotal: "32GiB", MemoryAllocated: "32GiB"},
				{HostName: "unknown", AcceleratorTotal: 8, AcceleratorFree: 1},
			}
			if kind == "vc" {
				result := &VCResourceUsageResult{}
				for _, item := range items {
					u := platform.VirtualClusterNodeResourceUsage{}
					u.Usage.Total.Device = "8"
					u.Usage.Available.Device = "1"
					u.Usage.Total.CPU = item.CPUTotal
					u.Usage.Allocated.CPU = item.CPUAllocated
					u.Usage.Total.Memory = item.MemoryTotal
					u.Usage.Allocated.Memory = item.MemoryAllocated
					result.Items = append(result.Items, VCNodeResourceUsageItem{HostName: item.HostName, Usage: u})
				}
				result.FilterFreeNodes()
				if len(result.Items) != 1 || result.Items[0].HostName != "free" {
					t.Fatal(result.Items)
				}
			} else if kind == "queue" {
				r := &SSPQueueNodeUsageResult{Items: items}
				r.FilterFreeNodes()
				if len(r.Items) != 1 || r.Items[0].HostName != "free" {
					t.Fatal(r.Items)
				}
			} else {
				r := &SSPWorkspaceNodeUsageResult{Items: items}
				r.FilterFreeNodes()
				if len(r.Items) != 1 || r.Items[0].HostName != "free" {
					t.Fatal(r.Items)
				}
			}
		})
	}
}
