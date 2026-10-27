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
		if result.SharedVCPool && result.FreeOnly {
			fmt.Fprintf(os.Stdout, "本队列候选节点 [%s] / VC [%s]\n", emptyDash(result.Queue.Name), emptyDash(result.Queue.VCluster))
			fmt.Fprintln(os.Stdout, "仅保留本队列已占用和公共池未归属节点；排除其他队列和未知归属，不把其他队列节点上的碎片算作本队列候选。")
			if ownership := result.Ownership; ownership != nil {
				fmt.Fprintf(os.Stdout, "VC 共 %d 台：本队列占用 %d，公共池未归属 %d，其他队列 %d，归属未知 %d；按归属保留 %d 台。\n", result.NodeCount, ownership.OwnedNodes, ownership.UnassignedNodes, ownership.OtherNodes, ownership.UnknownNodes, ownership.EligibleNodes)
			}
		} else if result.SharedVCPool {
			fmt.Fprintf(os.Stdout, "VC 共享候选池 [%s] / 队列 [%s]\n", emptyDash(result.Queue.VCluster), emptyDash(result.Queue.Name))
			fmt.Fprintln(os.Stdout, "ELASTIC 按需使用共享池，已分配节点会被优先复用，非队列独占资源；候选池余量不等于队列剩余配额。")
		} else {
			fmt.Fprintf(os.Stdout, "队列节点 [%s]\n", emptyDash(result.Queue.Name))
		}
		fmt.Fprintln(os.Stdout, "ALLOC/TOTAL = 已申请资源/节点可分配资源；CPU/内存优先采用 Kubernetes allocatable，与上方队列配额口径不同。")
		if result.FreeOnly {
			fmt.Fprintln(os.Stdout, "--free 再筛选 CPU、内存均有余量且加速卡未满（如有）的节点；节点筛选不校验队列配额，不保证可调度。队列剩余额度见上表 REMAINING，配额表不受筛选影响。")
		}
		for _, warning := range result.Warnings {
			fmt.Fprintf(os.Stdout, "警告：%s\n", warning)
		}
		if result.NodeUsageError != "" {
			fmt.Fprintln(os.Stdout, "节点用量未能读取，不能将其视为空池或没有空闲节点。")
		} else if result.OwnershipError != "" {
			fmt.Fprintln(os.Stdout, "节点归属未能核实，未将未知节点计入公共池；保留上方队列配额，不能据此判断没有空闲节点。")
		} else {
			count := result.NodeCount
			if count == 0 {
				count = len(result.Items)
			}
			if result.SharedVCPool && result.FreeOnly && result.Ownership != nil {
				fmt.Fprintf(os.Stdout, "归属候选共 %d 台，本次显示 %d 台。\n", result.Ownership.EligibleNodes, len(result.Items))
			} else {
				fmt.Fprintf(os.Stdout, "节点范围共 %d 台，本次显示 %d 台。\n", count, len(result.Items))
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
