package output

import (
	"fmt"
	"golang.org/x/term"
	"os"
	"rayctl/internal/service"
	"strings"
)

func PrintNodeList(nodes []service.NodeListItem, resolvedSelector string, total int, start int, end int, limit int, showProdRole bool, longOutput bool) {
	if captureJSON(map[string]any{"items": nodes, "total": total, "start": start, "end": end, "limit": limit, "selector": resolvedSelector}) {
		return
	}
	if resolvedSelector == "" {
		fmt.Fprintln(os.Stdout, "Selector: <all nodes>")
	} else {
		fmt.Fprintf(os.Stdout, "Selector: %s\n", truncateForWidth(resolvedSelector, terminalWidth()-10))
	}

	rows := make([][]string, 0, len(nodes))
	noWrapCells := make([][2]int, 0, len(nodes)*7)
	for rowIdx, node := range nodes {
		usage := emptyDash(firstNodeListValue(node.Usage, node.ProdRole))
		cluster := emptyDash(firstNodeListValue(node.SSPClusterName, node.ClusterName))
		if longOutput {
			rows = append(rows, []string{node.Name, emptyDash(node.Model), usage, emptyDash(node.ProdRole), cluster, emptyDash(node.QueueName), emptyDash(node.Tenant)})
		} else {
			rows = append(rows, []string{
				node.Name,
				yesNoFromReady(node.Ready),
				yesNo(node.Schedulable == "Yes"),
				yesNo(node.Repair),
				usage,
				node.InternalIP,
				cluster,
			})
		}
		for colIdx := range rows[len(rows)-1] {
			noWrapCells = append(noWrapCells, [2]int{rowIdx, colIdx})
		}
	}

	_ = showProdRole // Kept for API compatibility; current usage is always displayed.
	headers := []string{"HOSTNAME", "RDY", "SCH", "RPR", "USE", "IP", "CLUSTER"}
	maxWidths := []int{24, 5, 5, 3, 18, 16, 40}
	minWidths := []int{19, 5, 5, 3, 11, 14, 16}
	if longOutput {
		headers = []string{"HOSTNAME", "MODEL", "USE", "PROD", "CLUSTER", "QUEUE", "TENANT"}
		maxWidths = []int{24, 28, 18, 18, 32, 48, 18}
		minWidths = []int{19, 12, 3, 8, 12, 18, 8}
	}

	printBoxTableWithOptions(
		headers,
		rows,
		maxWidths,
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   minWidths,
		},
	)

	if total == 0 {
		return
	}

	if limit > 0 {
		fmt.Fprintf(os.Stdout, "Showing %d-%d of %d nodes. Use -A to show all.\n", start, end, total)
	} else {
		fmt.Fprintf(os.Stdout, "Showing all %d nodes.\n", total)
	}
}

func firstNodeListValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func PrintNodeDescribe(details *service.NodeDescribe, debugTiming bool, clientDuration interface{}, longOutput ...bool) {
	long := len(longOutput) > 0 && longOutput[0]
	if captureJSON(details) {
		return
	}
	if long && details != nil {
		printNodeIncrement(details)
		return
	}
	headers := []string{"HOST"}
	row := []string{details.Hostname}
	maxWidths := []int{24}
	showVC := details.Workloads == nil || long
	if showVC {
		headers = append(headers, "VC")
		row = append(row, emptyDash(details.VClusterName))
		maxWidths = append(maxWidths, 20)
	}
	headers = append(headers, "QUEUE", "RDY", "UNSCH", "RPR", "ACCEL")
	row = append(row,
		emptyDash(details.QueueName),
		yesNoFromReady(details.Ready),
		fmt.Sprintf("%t", details.Unschedulable),
		fmt.Sprintf("%t", details.Repair),
		details.GPUUsage,
	)
	maxWidths = append(maxWidths, 40, 3, 5, 5, 8)
	noWrap := makeNoWrapCells([2]int{0, 0})
	colors := map[string]string{}
	if showVC {
		noWrap[cellKey(0, 1)] = struct{}{}
	}
	if len(details.RDMAResources) > 0 {
		if nodeRDMACapacityZero(details.RDMAResources) && nodeColorEnabled() {
			colors[cellKey(0, len(headers))] = "\x1b[31m"
		}
		headers = append(headers, "RDMA")
		row = append(row, formatNodeRDMAUsage(details.RDMAResources))
		maxWidths = append(maxWidths, 18)
	}
	headers = append(headers, "CPU", "MEM", "PODS")
	row = append(row, details.CPUUsage, details.MemoryUsage, fmt.Sprintf("%d", details.MatchedPodCount))
	maxWidths = append(maxWidths, 12, 12, 5)

	printBoxTableWithOptions(
		headers,
		[][]string{row},
		maxWidths,
		tableOptions{noWrapCells: noWrap, cellColors: colors},
	)

	fmt.Fprintln(os.Stdout)
	if details.Workloads != nil {
		printNodeWorkloads(details.Workloads, long)
	} else if nodeHasECSDetails(details.PodDetails) {
		podRows := make([][]string, 0, len(details.PodDetails))
		for _, pod := range details.PodDetails {
			podRows = append(podRows, []string{pod.Pod, emptyDash(pod.Type), emptyDash(pod.Workload)})
		}
		printBoxTableWithMaxWidths([]string{"PODS", "TYPE", "WORKLOAD"}, podRows, []int{52, 6, 44})
	} else {
		podRows := make([][]string, 0, maxInt(1, len(details.Pods)))
		if len(details.Pods) == 0 {
			podRows = append(podRows, []string{"-"})
		} else {
			for _, pod := range details.Pods {
				podRows = append(podRows, []string{pod})
			}
		}
		printBoxTableWithMaxWidths(
			[]string{"PODS"},
			podRows,
			[]int{80},
		)
	}
	for _, warning := range details.Warnings {
		fmt.Fprintln(os.Stderr, "Warning: "+warning)
	}

	if debugTiming {
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Timings:")
		printBoxTable(
			[]string{"STEP", "DURATION"},
			[][]string{
				{"client init", fmt.Sprintf("%v", clientDuration)},
				{"get node", details.Timings.GetNode.String()},
				{"list namespaces", details.Timings.ListNamespaces.String()},
				{"list pods", details.Timings.ListPods.String()},
				{"summarize", details.Timings.Summarize.String()},
				{"resolve vc", details.Timings.ResolveVC.String()},
				{"resolve queue", details.Timings.ResolveQueue.String()},
				{"service total", details.Timings.Total.String()},
			},
		)
	}
}

func nodeHasECSDetails(pods []service.NodePodDetail) bool {
	for _, pod := range pods {
		if pod.Type == "ECS" {
			return true
		}
	}
	return false
}

func formatNodeRDMAUsage(resources []service.NodeExtendedResource) string {
	values := make([]string, 0, len(resources))
	for _, item := range resources {
		usage := fmt.Sprintf("%s/%s", emptyDash(item.Requested), emptyDash(item.Allocatable))
		if item.Capacity != "" && item.Capacity != item.Allocatable {
			usage += " cap=" + item.Capacity
		}
		values = append(values, usage)
	}
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}

func nodeRDMACapacityZero(resources []service.NodeExtendedResource) bool {
	for _, item := range resources {
		if item.Capacity == "0" || item.Allocatable == "0" {
			return true
		}
	}
	return false
}

func nodeColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if forced := os.Getenv("CLICOLOR_FORCE"); forced != "" && forced != "0" {
		return true
	}
	return os.Getenv("TERM") != "dumb" && term.IsTerminal(int(os.Stdout.Fd()))
}

func PrintNodeMutationResult(result *service.NodeMutationResult) {
	PrintNodeMutationResults([]*service.NodeMutationResult{result})
}

func PrintNodeMutationResults(results []*service.NodeMutationResult) {
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		rows = append(rows, []string{result.Name, result.Action, result.Schedulable, emptyDash(result.RepairLabel)})
	}
	printBoxTable(
		[]string{"NODE", "ACTION", "SCHEDULABLE", "REPAIR-LABEL"},
		rows,
	)
}

func PrintUserDetail(result *service.UserGetResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"ID", emptyDash(result.ID)},
			{"USERNAME", emptyDash(result.Username)},
			{"NAME", emptyDash(result.Name)},
			{"TENANT CODE", emptyDash(result.TenantCode)},
			{"STATUS", emptyDash(result.Status)},
			{"SOURCE", emptyDash(result.Source)},
			{"GROUPS", userGroupNames(result.Groups)},
		},
		[]int{14, 72},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
				[2]int{4, 1},
				[2]int{5, 1},
				[2]int{6, 1},
			),
			minWidths: []int{10, 20},
		},
	)

	if len(result.Workloads) > 0 {
		fmt.Fprintln(os.Stdout)
		printUserWorkloadTable(result.Workloads)
	} else if len(result.Jobs) > 0 {
		fmt.Fprintln(os.Stdout)
		jobRows := make([][]string, 0, maxInt(1, len(result.Jobs)))
		for _, job := range result.Jobs {
			jobRows = append(jobRows, []string{
				emptyDash(job.ClusterName),
				emptyDash(job.JobName),
				emptyDash(job.Status),
				emptyDash(job.CreatedAtShort),
			})
		}
		printBoxTableWithOptions(
			[]string{"分区名", "任务", "状态", "创建时间"},
			jobRows,
			[]int{24, 64, 10, 11},
			tableOptions{
				noWrapCells: makeNoWrapCells(append(
					noWrapCellsForSingleColumn(len(jobRows), 2),
					noWrapCellsForSingleColumn(len(jobRows), 3)...,
				)...),
				minWidths: []int{12, 20, 7, 11},
			},
		)
	}
	printUserWorkloadWarnings(result.WorkloadWarnings)
}

func PrintUserList(items []service.UserListItem, long bool) {
	if captureJSON(items) {
		return
	}
	headers := []string{"USERNAME", "NAME", "STATUS", "SOURCE"}
	widths := []int{30, 24, 10, 12}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{emptyDash(item.Username), emptyDash(item.Name), emptyDash(item.Status), emptyDash(item.Source)}
		if long {
			row = append(row, emptyDash(item.ID), emptyDash(item.TenantCode), emptyDash(item.CreateTime))
		}
		rows = append(rows, row)
	}
	if long {
		headers = append(headers, "ID", "TENANT CODE", "CREATED")
		widths = append(widths, 36, 18, 22)
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for i := range row {
			row[i] = "-"
		}
		rows = append(rows, row)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{})
	fmt.Fprintf(os.Stdout, "\n共 %d 个用户。\n", len(items))
}

func userGroupNames(groups []service.AuthGroupItem) string {
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		names = append(names, firstNonEmptyOutput(group.DisplayName, group.Name, group.PosixGroupName, group.ID))
	}
	return joinOrDash(names)
}

func PrintUserSummary(results []*service.UserGetResult, includeJobs bool) {
	if captureJSON(results) {
		return
	}
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		rows = append(rows, []string{emptyDash(result.Username), emptyDash(result.ID), emptyDash(result.Name), emptyDash(result.TenantCode), userGroupNames(result.Groups)})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions([]string{"USERNAME", "ID", "NAME", "TENANT", "GROUPS"}, rows, []int{32, 36, 20, 16, 72}, tableOptions{minWidths: []int{18, 36, 10, 10, 18}})
	if !includeJobs {
		return
	}
	jobRows := make([][]string, 0)
	workloadRows := make([][]string, 0)
	for _, result := range results {
		for _, workload := range result.Workloads {
			workloadRows = append(workloadRows, append([]string{emptyDash(result.Username)}, userWorkloadRow(workload)...))
		}
		for _, job := range result.Jobs {
			jobRows = append(jobRows, []string{emptyDash(result.Username), emptyDash(job.ClusterName), emptyDash(job.JobName), emptyDash(job.Status), emptyDash(job.CreatedAtShort)})
		}
	}
	if len(workloadRows) > 0 {
		fmt.Fprintln(os.Stdout)
		printBoxTableWithOptions(
			[]string{"USER", "TYPE", "WORKLOAD", "STATE", "WORKSPACE", "QUEUE / VC", "CREATED"},
			workloadRows,
			[]int{20, 5, 46, 12, 28, 32, 19},
			tableOptions{minWidths: []int{10, 4, 18, 8, 12, 12, 16}},
		)
		for _, result := range results {
			printUserWorkloadWarnings(result.WorkloadWarnings)
		}
		return
	}
	if len(jobRows) == 0 {
		for _, result := range results {
			printUserWorkloadWarnings(result.WorkloadWarnings)
		}
		return
	}
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions([]string{"USERNAME", "VC", "JOB", "STATUS", "CREATED"}, jobRows, []int{24, 24, 56, 10, 11}, tableOptions{minWidths: []int{12, 12, 18, 8, 11}})
	for _, result := range results {
		printUserWorkloadWarnings(result.WorkloadWarnings)
	}
}

func printUserWorkloadTable(items []service.UserWorkloadItem) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, userWorkloadRow(item))
	}
	printBoxTableWithOptions(
		[]string{"TYPE", "WORKLOAD", "STATE", "WORKSPACE", "QUEUE / VC", "CREATED"},
		rows,
		[]int{5, 52, 12, 28, 32, 19},
		tableOptions{minWidths: []int{4, 18, 8, 12, 12, 16}},
	)
}

func userWorkloadRow(item service.UserWorkloadItem) []string {
	location := firstNonEmptyOutput(item.Queue, item.VCluster)
	if strings.TrimSpace(item.Queue) != "" && strings.TrimSpace(item.VCluster) != "" && !strings.EqualFold(strings.TrimSpace(item.Queue), strings.TrimSpace(item.VCluster)) {
		location = item.Queue + " / " + item.VCluster
	}
	return []string{
		emptyDash(item.Type), emptyDash(item.Name), emptyDash(item.State),
		emptyDash(item.Workspace), emptyDash(location), emptyDash(item.CreatedAt),
	}
}

func printUserWorkloadWarnings(warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(os.Stdout, "Warning: %s\n", warning)
	}
}
