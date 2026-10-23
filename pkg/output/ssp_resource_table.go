package output

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"rayctl/internal/service"
)

func PrintSSPClusterList(result *service.SSPClusterListResult) {
	if captureJSON(result) {
		return
	}
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
	if captureJSON(result) {
		return
	}
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
	if captureJSON(result) {
		return
	}
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

func PrintSSPWorkspaceDetail(result *service.SSPWorkspaceItem, longOutput bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	if longOutput {
		printDetailAnchor(result.Name, result.State, "", "", result.Region)
		rows := [][]string{}
		for _, queue := range result.Queues {
			rows = append(rows, []string{queue.Name, queue.UID})
		}
		if len(rows) > 0 {
			printBoxTable([]string{"QUEUE", "UID"}, rows)
		}
		return
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"WORKSPACE", emptyDash(result.Name)},
			{"STATE", emptyDash(result.State)},
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
		row := []string{emptyDash(queue.Name)}
		if longOutput {
			row = append(row, emptyDash(queue.UID))
		}
		row = append(row, emptyDash(queue.State), emptyDash(queue.Type), emptyDash(queue.Cluster), emptyDash(queue.VCluster), emptyDash(queue.Namespace))
		rows = append(rows, row)
	}
	headers := []string{"QUEUE"}
	widths := []int{38}
	minWidths := []int{20}
	if longOutput {
		headers = append(headers, "UID")
		widths = append(widths, 36)
		minWidths = append(minWidths, 36)
	}
	headers = append(headers, "STATE", "TYPE", "CLUSTER", "VC", "NAMESPACE")
	widths = append(widths, 12, 20, 30, 30, 38)
	minWidths = append(minWidths, 8, 10, 16, 16, 20)
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for index := range row {
			row[index] = "-"
		}
		rows = append(rows, row)
	}
	printBoxTableWithOptions(
		headers,
		rows,
		widths,
		tableOptions{
			minWidths:   minWidths,
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 0)...),
		},
	)
}

func PrintSSPQueueList(result *service.SSPQueueListResult) {
	if captureJSON(result) {
		return
	}
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
	fmt.Fprintf(os.Stdout, "\n本次共 %d 条。\n", len(result.Items))
}

func PrintSSPQueueDetail(result *service.SSPQueueItem, longOutput bool) {
	if captureJSON(result) {
		return
	}
	if longOutput && result != nil {
		printQueueIncrement(result)
		return
	}
	if result == nil {
		return
	}
	rows := [][]string{
		{"QUEUE", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"TYPE", emptyDash(result.Type)},
		{"WORKSPACE", emptyDash(result.Workspace)},
		{"VC", emptyDash(result.VCluster)},
		{"空闲资源借出", emptyDash(result.SpotLending)},
		{"排队策略", emptyDash(result.DequeuePolicy)},
		{"SUBSCRIPTION", emptyDash(result.Subscription)},
		{"RESOURCE GROUP", emptyDash(result.ResourceGroup)},
		{"REGION", emptyDash(result.Region)},
		{"PROFILE", emptyDash(result.Profile)},
		{"CREATED", emptyDash(result.CreatedAt)},
		{"UPDATED", emptyDash(result.UpdatedAt)},
	}
	if longOutput {
		rows = append(rows,
			[]string{"WORKSPACE UID", emptyDash(result.WorkspaceUID)},
			[]string{"NODE COUNT", strconv.Itoa(result.NodeCount)},
			[]string{"MACHINE TYPES", emptyDash(result.MachineTypes)},
			[]string{"MAX NODES", emptyDash(result.MaxNodes)},
			[]string{"CAPABILITY", emptyDash(result.Capability)},
		)
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{18, 76},
		tableOptions{minWidths: []int{14, 24}},
	)
}

func PrintSSPQueueWorkloads(results []*service.SSPQueueWorkloadResult) {
	if captureJSON(results) {
		return
	}
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

func PrintSSPAITList(result *service.SSPCatalogListResult) {
	if captureJSON(result) {
		return
	}
	printSSPCatalogList(result, false, false, true)
}

func PrintSSPAIDList(result *service.SSPCatalogListResult, longOutput bool) {
	if captureJSON(result) {
		return
	}
	printSSPCatalogList(result, longOutput, longOutput, longOutput)
}

func printSSPCatalogList(result *service.SSPCatalogListResult, includeNode bool, includeResource bool, includeCreated bool) {
	rows := make([][]string, 0)
	total := 0
	if result != nil {
		total = len(result.Items)
		for _, item := range result.Items {
			row := []string{
				emptyDash(item.Name), emptyDash(item.State), emptyDash(item.Workspace),
				emptyDash(item.Queue), emptyDash(item.Creator),
			}
			if result.AID {
				count := "-"
				if item.Restarts != nil {
					count = fmt.Sprint(*item.Restarts)
				}
				row = append(row, count)
			}
			if includeNode {
				row = append(row, emptyDash(item.Node))
			}
			if includeResource {
				row = append(row, emptyDash(item.Resource))
			}
			if includeCreated {
				value := item.CreatedAt
				if result.TimeLabel == "FAILED" {
					value = item.EndedAt
				}
				row = append(row, emptyDash(value))
			}
			rows = append(rows, row)
		}
	}
	headers := []string{"NAME", "STATE", "WORKSPACE", "QUEUE", "CREATOR"}
	maxWidths := []int{48, 16, 38, 48, 22}
	minWidths := []int{28, 12, 22, 26, 14}
	if result != nil && result.AID {
		headers = append(headers, "RESTARTS")
		maxWidths = append(maxWidths, 8)
		minWidths = append(minWidths, 8)
	}
	if includeNode {
		headers = append(headers, "NODE")
		maxWidths = append(maxWidths, 30)
		minWidths = append(minWidths, 20)
	}
	if includeResource {
		headers = append(headers, "RESOURCE")
		maxWidths = append(maxWidths, 58)
		minWidths = append(minWidths, 24)
	}
	if includeCreated {
		header := "CREATED"
		if result != nil && result.TimeLabel != "" {
			header = result.TimeLabel
		}
		headers = append(headers, header)
		maxWidths = append(maxWidths, 20)
		minWidths = append(minWidths, 19)
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for index := range row {
			row[index] = "-"
		}
		rows = append(rows, row)
	}
	printBoxTableWithOptions(
		headers, rows,
		maxWidths,
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), 0, 1, 2, 3, 4)...),
			minWidths:   minWidths,
		},
	)
	fmt.Fprintf(os.Stdout, "\n本次筛选共 %d 条。\n", total)
}

func PrintSSPQueueNodeList(result *service.SSPQueueNodeListResult, longOutput bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	rows := [][]string{
		{"QUEUE", emptyDash(result.Queue.Name)},
		{"QUEUE UID", emptyDash(result.Queue.UID)},
		{"TYPE", emptyDash(result.Queue.Type)},
		{"WORKSPACE", emptyDash(result.Queue.Workspace)},
		{"VC", emptyDash(result.Queue.VCluster)},
		{"NODE COUNT", strconv.Itoa(len(result.Items))},
	}
	if result.SharedVCPool {
		rows = append(rows,
			[]string{"NODE SCOPE", "VC 共享候选池（非队列独占节点）"},
			[]string{"调度说明", "ELASTIC 动态分配节点，已分配节点会被优先复用"},
		)
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{16, 76},
		tableOptions{minWidths: []int{12, 24}},
	)
	fmt.Fprintln(os.Stdout)
	printVCNodeItems(result.Items, longOutput, false)
}

func PrintSSPQueueNodeUsage(results []*service.SSPQueueNodeUsageResult) {
	if captureJSON(results) {
		return
	}
	multipleQueues := len(results) > 1
	showAccelerator := sspQueueUsageHasAccelerator(results)
	showRDMA := sspQueueUsageHasRDMA(results)
	rows := make([][]string, 0)
	for _, result := range results {
		if result != nil && result.SharedVCPool {
			fmt.Fprintf(
				os.Stdout,
				"说明 [%s]: ELASTIC 动态分配节点；下表展示 VC 共享候选池，已分配节点会被优先复用，并非队列独占资源。\n\n",
				emptyDash(result.Queue.Name),
			)
		}
	}
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
			}
			if showAccelerator {
				row = append(row, emptyDash(accelerator))
			}
			if showRDMA {
				row = append(row, formatPlatformResourcePair(item.RDMAAllocated, item.RDMATotal))
			}
			row = append(row, emptyDash(cpu), emptyDash(memory))
			if multipleQueues {
				row = append([]string{emptyDash(result.Queue.Name)}, row...)
			}
			rows = append(rows, row)
		}
	}
	headers := []string{"HOST", "IP", "STATE"}
	widths := []int{20, 15, 10}
	minimums := []int{20, 15, 8}
	emptyRow := []string{"-", "-", "-"}
	if showAccelerator {
		headers = append(headers, "ACCEL ALLOC/TOTAL")
		widths = append(widths, 18)
		minimums = append(minimums, 17)
		emptyRow = append(emptyRow, "-")
	}
	if showRDMA {
		headers = append(headers, "RDMA ALLOC/TOTAL")
		widths = append(widths, 17)
		minimums = append(minimums, 16)
		emptyRow = append(emptyRow, "-")
	}
	headers = append(headers, "CPU ALLOC/TOTAL", "MEMORY ALLOC/TOTAL")
	widths = append(widths, 18, 22)
	minimums = append(minimums, 15, 20)
	emptyRow = append(emptyRow, "-", "-")
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

func PrintSSPWorkspaceNodeUsage(results []*service.SSPWorkspaceNodeUsageResult) {
	if captureJSON(results) {
		return
	}
	multipleWorkspaces := len(results) > 1
	showAccelerator := false
	showRDMA := false
	rows := make([][]string, 0)
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			showAccelerator = showAccelerator || sspNodeUsageItemHasAccelerator(item)
			showRDMA = showRDMA || sspNodeUsageItemHasRDMA(item)
		}
		if result.SharedVCPools {
			fmt.Fprintf(os.Stdout, "说明 [%s]: ELASTIC queue 的节点来自 VC 共享候选池；相同节点已按 IP/hostname 合并。\n\n", emptyDash(result.Workspace.Name))
		}
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			row := []string{emptyDash(compactWorkspaceQueueNames(result.Workspace.Name, item.Queues)), emptyDash(item.HostName), emptyDash(item.HostIP), emptyDash(item.State)}
			if showAccelerator {
				row = append(row, emptyDash(formatPlatformResourcePair(item.AcceleratorAllocated, item.AcceleratorTotalText)))
			}
			if showRDMA {
				row = append(row, formatPlatformResourcePair(item.RDMAAllocated, item.RDMATotal))
			}
			row = append(row,
				emptyDash(formatPlatformResourcePair(item.CPUAllocated, item.CPUTotal)),
				emptyDash(formatPlatformResourcePair(item.MemoryAllocated, item.MemoryTotal)),
			)
			if multipleWorkspaces {
				row = append([]string{emptyDash(result.Workspace.Name)}, row...)
			}
			rows = append(rows, row)
		}
	}
	headers := []string{"QUEUES", "HOST", "IP", "STATE"}
	widths := []int{34, 20, 15, 10}
	minimums := []int{18, 20, 15, 8}
	emptyRow := []string{"-", "-", "-", "-"}
	if showAccelerator {
		headers = append(headers, "ACCEL ALLOC/TOTAL")
		widths = append(widths, 18)
		minimums = append(minimums, 17)
		emptyRow = append(emptyRow, "-")
	}
	if showRDMA {
		headers = append(headers, "RDMA ALLOC/TOTAL")
		widths = append(widths, 17)
		minimums = append(minimums, 16)
		emptyRow = append(emptyRow, "-")
	}
	headers = append(headers, "CPU ALLOC/TOTAL", "MEMORY ALLOC/TOTAL")
	widths = append(widths, 18, 22)
	minimums = append(minimums, 15, 20)
	emptyRow = append(emptyRow, "-", "-")
	if multipleWorkspaces {
		headers = append([]string{"WORKSPACE"}, headers...)
		widths = append([]int{34}, widths...)
		minimums = append([]int{18}, minimums...)
		emptyRow = append([]string{"-"}, emptyRow...)
	}
	if len(rows) == 0 {
		rows = append(rows, emptyRow)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{minWidths: minimums})
}

func compactWorkspaceQueueNames(workspace string, queues string) string {
	workspace = strings.TrimSpace(strings.TrimPrefix(workspace, "ws-"))
	environment, _, found := strings.Cut(workspace, "-")
	if !found || environment == "" {
		return queues
	}
	prefix := "queue-" + environment + "-"
	parts := strings.Split(queues, ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
		if strings.HasPrefix(strings.ToLower(parts[index]), strings.ToLower(prefix)) {
			parts[index] = parts[index][len(prefix):]
		}
	}
	return strings.Join(parts, ", ")
}

func sspNodeUsageItemHasRDMA(item service.SSPQueueNodeUsageItem) bool {
	return platformResourceAmountIsPositive(item.RDMATotal) || platformResourceAmountIsPositive(item.RDMAAllocated)
}

func sspNodeUsageItemHasAccelerator(item service.SSPQueueNodeUsageItem) bool {
	return item.AcceleratorTotal > 0 || item.AcceleratorFree > 0 ||
		platformResourceAmountIsPositive(item.AcceleratorTotalText) ||
		platformResourceAmountIsPositive(item.AcceleratorAllocated) ||
		platformResourceAmountIsPositive(item.Accelerator)
}

func sspQueueUsageHasRDMA(results []*service.SSPQueueNodeUsageResult) bool {
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			if sspNodeUsageItemHasRDMA(item) {
				return true
			}
		}
	}
	return false
}

func sspQueueUsageHasAccelerator(results []*service.SSPQueueNodeUsageResult) bool {
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			if sspNodeUsageItemHasAccelerator(item) {
				return true
			}
		}
	}
	return false
}
