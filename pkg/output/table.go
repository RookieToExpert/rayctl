package output

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"rayctl/internal/service"
)

func PrintNodeList(nodes []service.NodeListItem, resolvedSelector string, total int, start int, end int, limit int) {
	if resolvedSelector == "" {
		fmt.Fprintln(os.Stdout, "Selector: <all nodes>")
	} else {
		fmt.Fprintf(os.Stdout, "Selector: %s\n", truncateForWidth(resolvedSelector, terminalWidth()-10))
	}

	rows := make([][]string, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, []string{
			truncateCell(node.Name, 22),
			node.Ready,
			node.Schedulable,
			formatRepair(node.UsageType),
			truncateCell(node.InternalIP, 15),
			node.ClusterName,
		})
	}

	printBoxTableWithMaxWidths(
		[]string{"HOST", "RDY", "SCH", "REPAIR", "IP", "CLUSTERNAME"},
		rows,
		[]int{22, 5, 5, 6, 15, 24},
	)

	if total == 0 {
		return
	}

	if limit > 0 {
		fmt.Fprintf(os.Stdout, "Showing %d-%d of %d nodes. Use -A to show all or -l N to change the limit.\n", start, end, total)
	} else {
		fmt.Fprintf(os.Stdout, "Showing all %d nodes.\n", total)
	}
}

func PrintNodeDescribe(details *service.NodeDescribe, debugTiming bool, clientDuration interface{}) {
	lines := details.Pods
	if len(lines) == 0 {
		lines = []string{"-"}
	}

	rows := make([][]string, 0, len(lines))
	for i, pod := range lines {
		if i == 0 {
			rows = append(rows, []string{
				details.Hostname,
				fmt.Sprintf("%t", details.Unschedulable),
				fmt.Sprintf("%t", details.Repair),
				details.GPUUsage,
				details.CPUUsage,
				details.MemoryUsage,
				pod,
			})
		} else {
			rows = append(rows, []string{"", "", "", "", "", "", pod})
		}
	}

	printBoxTable(
		[]string{"HOSTNAME", "UNSCHEDULABLE", "REPAIR", "GPU ALLOCATED/COUNT", "CPU ALLOCATED/COUNT", "MEMORY ALLOCATED/COUNT", "PODS"},
		rows,
	)

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
				{"service total", details.Timings.Total.String()},
			},
		)
	}
}

func PrintNodeMutationResult(result *service.NodeMutationResult) {
	printBoxTable(
		[]string{"NODE", "ACTION", "SCHEDULABLE", "REPAIR-LABEL"},
		[][]string{
			{result.Name, result.Action, result.Schedulable, emptyDash(result.RepairLabel)},
		},
	)
}

func PrintJobDetail(result *service.JobGetResult) {
	summaryRows := [][]string{
		{"JOB", result.Name},
		{"NAMESPACE", result.Namespace},
		{"UID", result.UID},
		{"SUBMITTER", result.Submitter},
		{"PODGROUP", result.PodGroupName},
		{"INSPECT POD", result.InspectPod},
		{"NODES", joinOrDash(result.Nodes)},
	}
	printBoxTable([]string{"FIELD", "VALUE"}, summaryRows)

	fmt.Fprintln(os.Stdout)
	podRows := make([][]string, 0, len(result.Pods))
	for _, pod := range result.Pods {
		podRows = append(podRows, []string{
			pod.Name,
			emptyDash(pod.TaskSpec),
			emptyDash(pod.TaskIndex),
			emptyDash(pod.Phase),
			emptyDash(pod.NodeName),
		})
	}
	if len(podRows) == 0 {
		podRows = [][]string{{"-", "-", "-", "-", "-"}}
	}
	printBoxTableWithMaxWidths(
		[]string{"POD", "TASK", "INDEX", "PHASE", "NODE"},
		podRows,
		[]int{48, 12, 8, 12, 24},
	)

	fmt.Fprintln(os.Stdout)
	eventRows := make([][]string, 0, len(result.RecentEvents))
	for _, event := range result.RecentEvents {
		eventRows = append(eventRows, []string{event.Time, event.Type, event.Reason, event.Message})
	}
	printBoxTableWithMaxWidths(
		[]string{"LATEST EVENT TIME", "TYPE", "REASON", "MESSAGE"},
		eventRows,
		[]int{20, 10, 24, 72},
	)

	fmt.Fprintln(os.Stdout)
	logRows := make([][]string, 0, len(result.RecentLogLines))
	for _, line := range result.RecentLogLines {
		logRows = append(logRows, []string{line})
	}
	printBoxTableWithMaxWidths(
		[]string{"LATEST LOGS"},
		logRows,
		[]int{110},
	)
}

func PrintPodGroupDetail(result *service.PodGroupGetResult) {
	summaryRows := [][]string{
		{"PODGROUP", result.Name},
		{"NAMESPACE", result.Namespace},
		{"STATUS", result.Status},
		{"MIN MEMBER", result.MinMember},
		{"QUEUE", result.Queue},
		{"CREATED AT", result.CreatedAt},
	}
	printBoxTable([]string{"FIELD", "VALUE"}, summaryRows)

	fmt.Fprintln(os.Stdout)
	eventRows := make([][]string, 0, len(result.RecentEvents))
	for _, event := range result.RecentEvents {
		eventRows = append(eventRows, []string{event.Time, event.Type, event.Reason, event.Message})
	}
	printBoxTableWithMaxWidths(
		[]string{"LATEST EVENT TIME", "TYPE", "REASON", "MESSAGE"},
		eventRows,
		[]int{20, 10, 24, 72},
	)
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func printBoxTable(headers []string, rows [][]string) {
	printBoxTableWithMaxWidths(headers, rows, nil)
}

func printBoxTableWithMaxWidths(headers []string, rows [][]string, maxWidths []int) {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = textWidth(header)
		if i < len(maxWidths) && maxWidths[i] > 0 && widths[i] > maxWidths[i] {
			widths[i] = maxWidths[i]
		}
	}

	for _, row := range rows {
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			cellLen := textWidth(cell)
			if i < len(maxWidths) && maxWidths[i] > 0 && cellLen > maxWidths[i] {
				cellLen = maxWidths[i]
			}
			if cellLen > widths[i] {
				widths[i] = cellLen
			}
		}
	}

	fmt.Fprintln(os.Stdout, borderLine("┌", "┬", "┐", widths))
	fmt.Fprintln(os.Stdout, renderRow(headers, widths))
	fmt.Fprintln(os.Stdout, borderLine("├", "┼", "┤", widths))
	for _, row := range rows {
		fmt.Fprintln(os.Stdout, renderRow(row, widths))
	}
	fmt.Fprintln(os.Stdout, borderLine("└", "┴", "┘", widths))
}

func borderLine(left string, middle string, right string, widths []int) string {
	var b strings.Builder
	b.WriteString(left)
	for i, width := range widths {
		b.WriteString(strings.Repeat("─", width+2))
		if i == len(widths)-1 {
			b.WriteString(right)
		} else {
			b.WriteString(middle)
		}
	}
	return b.String()
}

func renderRow(row []string, widths []int) string {
	var b strings.Builder
	b.WriteString("│")
	for i, width := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		cell = truncateCell(cell, width)
		padding := width - textWidth(cell)
		if padding < 0 {
			padding = 0
		}
		b.WriteString(" ")
		b.WriteString(cell)
		b.WriteString(strings.Repeat(" ", padding+1))
		b.WriteString("│")
	}
	return b.String()
}

func truncateCell(value string, width int) string {
	if width <= 0 || textWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "."
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}

	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width-3]) + "..."
}

func formatRepair(v string) string {
	return fmt.Sprintf("%t", strings.TrimSpace(v) == "repair")
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}

func terminalWidth() int {
	if value := strings.TrimSpace(os.Getenv("COLUMNS")); value != "" {
		if width, err := strconv.Atoi(value); err == nil && width > 0 {
			return width
		}
	}
	return 120
}

func truncateForWidth(value string, width int) string {
	if width <= 0 {
		return value
	}
	return truncateCell(value, width)
}

func textWidth(value string) int {
	if value == "" {
		return 0
	}
	return utf8.RuneCountInString(value)
}
