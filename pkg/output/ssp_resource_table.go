package output

import (
	"fmt"
	"os"
	"strconv"

	"rayctl/internal/service"
)

func PrintSSPClusterList(result *service.SSPClusterListResult) {
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.VCluster),
			strconv.Itoa(item.QueueCount), strconv.Itoa(item.NodeCount), emptyDash(item.Region),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"CLUSTER", "STATE", "VC", "QUEUES", "NODES", "REGION"},
		rows,
		[]int{34, 12, 34, 8, 8, 12},
		tableOptions{noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), 0, 2)...), minWidths: []int{18, 8, 18, 8, 8, 10}},
	)
}

func PrintSSPClusterDetail(result *service.SSPClusterItem) {
	if result == nil {
		return
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"CLUSTER", emptyDash(result.Name)},
			{"UID", emptyDash(result.UID)},
			{"STATE", emptyDash(result.State)},
			{"TYPE", emptyDash(result.Type)},
			{"VC", emptyDash(result.VCluster)},
			{"VC UID", emptyDash(result.VClusterUID)},
			{"QUEUE COUNT", strconv.Itoa(result.QueueCount)},
			{"NODE COUNT", strconv.Itoa(result.NodeCount)},
			{"READY / IDLE / UNHEALTHY", fmt.Sprintf("%d / %d / %d", result.ReadyNodes, result.IdleNodes, result.UnhealthyNodes)},
			{"INFRA TYPE", emptyDash(result.InfraType)},
			{"VPC UID", emptyDash(result.VPCUID)},
			{"SUBSCRIPTION", emptyDash(result.Subscription)},
			{"RESOURCE GROUP", emptyDash(result.ResourceGroup)},
			{"REGION", emptyDash(result.Region)},
			{"PROFILE", emptyDash(result.Profile)},
			{"CREATED", emptyDash(result.CreatedAt)},
			{"UPDATED", emptyDash(result.UpdatedAt)},
		},
		[]int{28, 76},
		tableOptions{minWidths: []int{18, 24}},
	)

	fmt.Fprintln(os.Stdout)
	resourceRows := make([][]string, 0, maxInt(1, len(result.Resources)))
	for _, resource := range result.Resources {
		resourceRows = append(resourceRows, []string{
			emptyDash(resource.ResourceType),
			formatPlatformResourcePair(resource.Allocated, resource.Total),
			emptyDash(resource.Unallocated),
			emptyDash(resource.Elastic),
			emptyDash(resource.Spot),
			emptyDash(resource.Unit),
		})
	}
	if len(resourceRows) == 0 {
		resourceRows = append(resourceRows, []string{"-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"RESOURCE", "ALLOC/TOTAL", "AVAILABLE", "ELASTIC", "SPOT", "UNIT"},
		resourceRows,
		[]int{14, 18, 14, 12, 12, 10},
		tableOptions{minWidths: []int{10, 14, 10, 8, 8, 8}},
	)

	fmt.Fprintln(os.Stdout)
	queueRows := make([][]string, 0, maxInt(1, len(result.Queues)))
	for _, queue := range result.Queues {
		queueRows = append(queueRows, []string{
			emptyDash(queue.Name), emptyDash(queue.State), emptyDash(queue.Type),
			emptyDash(queue.Workspace), strconv.Itoa(queue.NodeCount),
		})
	}
	if len(queueRows) == 0 {
		queueRows = append(queueRows, []string{"-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"QUEUE", "STATE", "TYPE", "WORKSPACE", "NODES"},
		queueRows,
		[]int{52, 12, 18, 40, 8},
		tableOptions{noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(queueRows), 0, 3)...), minWidths: []int{26, 8, 10, 20, 8}},
	)
}

func PrintSSPWorkspaceList(result *service.SSPWorkspaceListResult) {
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name),
			emptyDash(item.UID),
			emptyDash(item.State),
			emptyDash(item.VCluster),
			emptyDash(item.Region),
			emptyDash(item.Profile),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"WORKSPACE", "UID", "STATE", "VC", "REGION", "PROFILE"},
		rows,
		[]int{34, 36, 12, 30, 12, 16},
		tableOptions{minWidths: []int{20, 36, 8, 16, 10, 10}},
	)
}

func PrintSSPWorkspaceDetail(result *service.SSPWorkspaceItem) {
	if result == nil {
		return
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"WORKSPACE", emptyDash(result.Name)},
			{"UID", emptyDash(result.UID)},
			{"STATE", emptyDash(result.State)},
			{"VC", emptyDash(result.VCluster)},
			{"QUEUE COUNT", strconv.Itoa(len(result.Queues))},
			{"SUBSCRIPTION", emptyDash(result.Subscription)},
			{"RESOURCE GROUP", emptyDash(result.ResourceGroup)},
			{"REGION", emptyDash(result.Region)},
			{"PROFILE", emptyDash(result.Profile)},
			{"CREATED", emptyDash(result.CreatedAt)},
			{"UPDATED", emptyDash(result.UpdatedAt)},
		},
		[]int{18, 76},
		tableOptions{minWidths: []int{14, 24}},
	)
	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, maxInt(1, len(result.Queues)))
	for _, queue := range result.Queues {
		rows = append(rows, []string{
			emptyDash(queue.Name),
			emptyDash(queue.UID),
			emptyDash(queue.State),
			emptyDash(queue.Type),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"QUEUE", "UID", "STATE", "TYPE"},
		rows,
		[]int{38, 36, 12, 20},
		tableOptions{minWidths: []int{20, 36, 8, 10}},
	)
}

func PrintSSPQueueList(result *service.SSPQueueListResult) {
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name),
			emptyDash(item.State),
			emptyDash(item.Type),
			emptyDash(item.Workspace),
			emptyDash(item.VCluster),
			emptyDash(item.Region),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"QUEUE", "STATE", "TYPE", "WORKSPACE", "VC", "REGION"},
		rows,
		[]int{52, 12, 18, 40, 34, 12},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), 0, 3, 4)...),
			minWidths:   []int{28, 8, 10, 24, 18, 10},
		},
	)
}

func PrintSSPQueueDetail(result *service.SSPQueueItem) {
	if result == nil {
		return
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"QUEUE", emptyDash(result.Name)},
			{"UID", emptyDash(result.UID)},
			{"STATE", emptyDash(result.State)},
			{"TYPE", emptyDash(result.Type)},
			{"空闲资源借出", emptyDash(result.SpotLending)},
			{"排队策略", emptyDash(result.DequeuePolicy)},
			{"WORKSPACE", emptyDash(result.Workspace)},
			{"WORKSPACE UID", emptyDash(result.WorkspaceUID)},
			{"VC", emptyDash(result.VCluster)},
			{"NODE COUNT", strconv.Itoa(result.NodeCount)},
			{"SUBSCRIPTION", emptyDash(result.Subscription)},
			{"RESOURCE GROUP", emptyDash(result.ResourceGroup)},
			{"REGION", emptyDash(result.Region)},
			{"PROFILE", emptyDash(result.Profile)},
			{"CREATED", emptyDash(result.CreatedAt)},
			{"UPDATED", emptyDash(result.UpdatedAt)},
		},
		[]int{18, 76},
		tableOptions{minWidths: []int{14, 24}},
	)
}

func PrintSSPQueueWorkloads(results []*service.SSPQueueWorkloadResult) {
	multipleQueues := len(results) > 1
	rows := make([][]string, 0)
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			row := []string{
				formatSSPWorkloadType(item.Type),
				emptyDash(item.Name),
				emptyDash(item.State),
				emptyDash(item.Workspace),
				emptyDash(item.Priority),
				emptyDash(item.Creator),
				emptyDash(item.CreatedAt),
			}
			if multipleQueues {
				row = append([]string{emptyDash(result.Queue.Name)}, row...)
			}
			rows = append(rows, row)
		}
	}
	headers := []string{"TYPE", "NAME", "STATE", "WORKSPACE", "PRIORITY", "CREATOR", "CREATED"}
	widths := []int{10, 56, 14, 38, 12, 24, 20}
	minimums := []int{6, 32, 10, 20, 10, 14, 19}
	noWrapColumns := []int{1, 3, 5}
	emptyRow := []string{"-", "-", "-", "-", "-", "-", "-"}
	if multipleQueues {
		headers = append([]string{"QUEUE"}, headers...)
		widths = append([]int{44}, widths...)
		minimums = append([]int{22}, minimums...)
		noWrapColumns = []int{0, 2, 4, 6}
		emptyRow = append([]string{"-"}, emptyRow...)
	}
	if len(rows) == 0 {
		rows = append(rows, emptyRow)
	}
	noWrapCells := make([][2]int, 0)
	for _, column := range noWrapColumns {
		noWrapCells = append(noWrapCells, noWrapCellsForColumns(len(rows), column)...)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{minWidths: minimums, noWrapCells: makeNoWrapCells(noWrapCells...)})
}

func formatSSPWorkloadType(value string) string {
	switch value {
	case "trainingJob", "training-job":
		return "JOB"
	case "aid":
		return "AID"
	case "air":
		return "AIR"
	case "inferGateway", "infer-gateway":
		return "GW"
	default:
		return emptyDash(value)
	}
}

func PrintSSPQueueNodeList(result *service.SSPQueueNodeListResult, longOutput bool) {
	if result == nil {
		return
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"QUEUE", emptyDash(result.Queue.Name)},
			{"QUEUE UID", emptyDash(result.Queue.UID)},
			{"WORKSPACE", emptyDash(result.Queue.Workspace)},
			{"VC", emptyDash(result.Queue.VCluster)},
			{"NODE COUNT", strconv.Itoa(len(result.Items))},
		},
		[]int{16, 76},
		tableOptions{minWidths: []int{12, 24}},
	)
	fmt.Fprintln(os.Stdout)
	printVCNodeItems(result.Items, longOutput)
}

func PrintSSPQueueNodeUsage(results []*service.SSPQueueNodeUsageResult) {
	multipleQueues := len(results) > 1
	rows := make([][]string, 0)
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			accelerator := item.Accelerator
			cpu := item.CPU
			memory := item.Memory
			if item.AcceleratorTotalText != "" {
				accelerator = formatPlatformResourcePair(item.AcceleratorAllocated, item.AcceleratorTotalText)
			}
			if item.CPUTotal != "" {
				cpu = formatPlatformResourcePair(item.CPUAllocated, item.CPUTotal)
			}
			if item.MemoryTotal != "" {
				memory = formatPlatformResourcePair(item.MemoryAllocated, item.MemoryTotal)
			}
			row := []string{
				emptyDash(item.HostName),
				emptyDash(item.HostIP),
				emptyDash(item.State),
				emptyDash(accelerator),
				emptyDash(cpu),
				emptyDash(memory),
			}
			if multipleQueues {
				row = append([]string{emptyDash(result.Queue.Name)}, row...)
			}
			rows = append(rows, row)
		}
	}
	headers := []string{"HOST", "IP", "STATE", "ACCEL ALLOC/TOTAL", "CPU ALLOC/TOTAL", "MEMORY ALLOC/TOTAL"}
	widths := []int{20, 15, 10, 18, 18, 22}
	minimums := []int{20, 15, 8, 17, 15, 20}
	emptyRow := []string{"-", "-", "-", "-", "-", "-"}
	if multipleQueues {
		headers = append([]string{"QUEUE"}, headers...)
		widths = append([]int{34}, widths...)
		minimums = append([]int{18}, minimums...)
		emptyRow = append([]string{"-"}, emptyRow...)
	}
	if len(rows) == 0 {
		rows = append(rows, emptyRow)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{minWidths: minimums})
}
