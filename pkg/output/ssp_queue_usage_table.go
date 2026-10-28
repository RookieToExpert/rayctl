package output

import (
	"fmt"
	"os"
	"strings"

	"rayctl/internal/platform"
	"rayctl/internal/service"
)

func PrintSSPQueueNodeUsage(results []*service.SSPQueueNodeUsageResult) {
	if captureJSON(results) {
		return
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		fmt.Fprintf(os.Stdout, "队列配额 [%s]（%s）\n", emptyDash(result.Queue.Name), emptyDash(result.Queue.Type))
		if result.Quota == nil {
			fmt.Fprintln(os.Stdout, "队列配额未能读取。")
		} else {
			printSSPQueueQuota(result.Quota)
		}
		fmt.Fprintln(os.Stdout)
		if result.SharedVCPool && result.FreeOnly {
			fmt.Fprintf(os.Stdout, "本队列候选节点 [%s] / VC [%s]\n", emptyDash(result.Queue.Name), emptyDash(result.Queue.VCluster))
		} else if result.SharedVCPool {
			fmt.Fprintf(os.Stdout, "本队列占用节点 [%s] / VC [%s]\n", emptyDash(result.Queue.Name), emptyDash(result.Queue.VCluster))
		} else {
			fmt.Fprintf(os.Stdout, "队列节点 [%s]\n", emptyDash(result.Queue.Name))
		}
		for _, warning := range result.Warnings {
			fmt.Fprintf(os.Stdout, "警告：%s\n", warning)
		}
		if result.NodeUsageError != "" {
			fmt.Fprintln(os.Stdout, "节点用量未能读取。")
		} else if result.OwnershipError != "" {
			fmt.Fprintln(os.Stdout, "节点归属未能核实。")
		} else {
			if result.SharedVCPool && result.FreeOnly && result.Ownership != nil {
				owned, unassigned := 0, 0
				for _, item := range result.Items {
					if item.Ownership == "owned" {
						owned++
					} else if item.Ownership == "unassigned" {
						unassigned++
					}
				}
				fmt.Fprintf(os.Stdout, "共 %d 台：本队列 %d，公共池未归属 %d。\n", len(result.Items), owned, unassigned)
			} else {
				fmt.Fprintf(os.Stdout, "共 %d 台。\n", len(result.Items))
			}
			printSSPQueueUsageNodes(result)
		}
		fmt.Fprintln(os.Stdout)
	}
}

func printSSPQueueQuota(quota *platform.SSPQueueQuota) {
	rows := make([][]string, 0)
	appendResources := func(machineType string, resources []platform.SSPResourceSummary) {
		for _, resource := range resources {
			label := strings.TrimSpace(resource.ResourceType)
			if resource.Unit != "" {
				label += " (" + resource.Unit + ")"
			}
			rows = append(rows, []string{emptyDash(machineType), emptyDash(label), emptyDash(resource.Total), emptyDash(resource.Allocated), emptyDash(resource.Unallocated)})
		}
	}
	if len(quota.Resources) > 0 {
		machineType := "合计"
		if len(quota.MachineTypes) == 1 {
			machineType = quota.MachineTypes[0].MachineType
		}
		appendResources(machineType, quota.Resources)
	}
	if len(quota.Resources) == 0 || len(quota.MachineTypes) > 1 {
		for _, machineType := range quota.MachineTypes {
			appendResources(machineType.MachineType, machineType.Resources)
		}
	}
	printBoxTableWithOptions([]string{"MACHINE TYPE", "RESOURCE", "TOTAL", "ALLOCATED", "REMAINING"}, rows,
		[]int{20, 20, 12, 12, 12}, tableOptions{minWidths: []int{14, 12, 8, 9, 9}, noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), 0, 1, 2, 3, 4)...)})
}

func printSSPQueueUsageNodes(result *service.SSPQueueNodeUsageResult) {
	showOwnership := result.SharedVCPool && result.FreeOnly && result.Ownership != nil
	showAccelerator := sspQueueUsageHasAccelerator([]*service.SSPQueueNodeUsageResult{result})
	showRDMA := sspQueueUsageHasRDMA([]*service.SSPQueueNodeUsageResult{result})
	rows := make([][]string, 0, len(result.Items))
	for _, item := range result.Items {
		accelerator, cpu, memory := item.Accelerator, item.CPU, item.Memory
		if item.AcceleratorTotalText != "" {
			accelerator = formatPlatformResourcePair(item.AcceleratorAllocated, item.AcceleratorTotalText)
		}
		if item.CPUTotal != "" {
			cpu = formatPlatformResourcePair(item.CPUAllocated, item.CPUTotal)
		}
		if item.MemoryTotal != "" {
			memory = formatPlatformResourcePair(item.MemoryAllocated, item.MemoryTotal)
		}
		row := []string{emptyDash(item.HostName), emptyDash(item.HostIP), emptyDash(item.State)}
		if showOwnership {
			row = append(row, item.Ownership)
		}
		if showAccelerator {
			row = append(row, emptyDash(accelerator))
		}
		if showRDMA {
			row = append(row, formatPlatformResourcePair(item.RDMAAllocated, item.RDMATotal))
		}
		rows = append(rows, append(row, emptyDash(cpu), emptyDash(memory)))
	}
	headers := []string{"HOST", "IP", "STATE"}
	widths, minimums := []int{20, 15, 10}, []int{20, 15, 8}
	emptyRow := []string{"-", "-", "-"}
	if showOwnership {
		headers = append(headers, "OWNERSHIP")
		widths, minimums = append(widths, 12), append(minimums, 10)
		emptyRow = append(emptyRow, "-")
	}
	if showAccelerator {
		headers = append(headers, "ACCEL ALLOC/TOTAL")
		widths, minimums = append(widths, 18), append(minimums, 17)
		emptyRow = append(emptyRow, "-")
	}
	if showRDMA {
		headers = append(headers, "RDMA ALLOC/TOTAL")
		widths, minimums = append(widths, 17), append(minimums, 16)
		emptyRow = append(emptyRow, "-")
	}
	headers = append(headers, "CPU ALLOC/TOTAL", "MEMORY ALLOC/TOTAL")
	widths, minimums = append(widths, 18, 22), append(minimums, 15, 20)
	if len(rows) == 0 {
		rows = append(rows, append(emptyRow, "-", "-"))
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{minWidths: minimums})
}
