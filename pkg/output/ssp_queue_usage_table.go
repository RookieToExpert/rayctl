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
		fmt.Fprintf(os.Stdout, "队列配额 [%s]（%s；平台资源页口径）\n", emptyDash(result.Queue.Name), emptyDash(result.Queue.Type))
		if result.Quota == nil {
			fmt.Fprintln(os.Stdout, "队列配额未能读取，不能据此判断队列剩余额度。")
		} else {
			printSSPQueueQuota(result.Quota)
		}
		fmt.Fprintln(os.Stdout)
		if result.SharedVCPool {
			fmt.Fprintf(os.Stdout, "VC 共享候选池 [%s] / 队列 [%s]\n", emptyDash(result.Queue.VCluster), emptyDash(result.Queue.Name))
			fmt.Fprintln(os.Stdout, "ELASTIC 按需使用共享池，已分配节点会被优先复用，非队列独占资源；候选池余量不等于队列剩余配额。")
		} else {
			fmt.Fprintf(os.Stdout, "队列节点 [%s]\n", emptyDash(result.Queue.Name))
		}
		fmt.Fprintln(os.Stdout, "ALLOC/TOTAL = 已申请资源/节点可分配资源；CPU/内存优先采用 Kubernetes allocatable，与上方队列配额口径不同。")
		if result.FreeOnly {
			fmt.Fprintln(os.Stdout, "--free 仅筛选 CPU、内存均有余量且加速卡未满（如有）的节点；不校验队列配额，不保证可调度；上方配额表不受筛选影响。")
		}
		if result.NodeUsageError != "" {
			fmt.Fprintln(os.Stdout, "节点用量未能读取，不能将其视为空池或没有空闲节点。")
		} else {
			count := result.NodeCount
			if count == 0 {
				count = len(result.Items)
			}
			fmt.Fprintf(os.Stdout, "节点范围共 %d 台，本次显示 %d 台。\n", count, len(result.Items))
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
