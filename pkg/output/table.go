package output

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

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
			emptyDash(node.ProdRole),
			formatRepairFlag(node.Repair),
			truncateCell(node.InternalIP, 15),
			node.ClusterName,
		})
	}

	printBoxTableWithMaxWidths(
		[]string{"HOST", "RDY", "SCH", "PROD", "REPAIR", "IP", "CLUSTERNAME"},
		rows,
		[]int{22, 5, 5, 14, 6, 15, 24},
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
	printBoxTableWithMaxWidths(
		[]string{"HOSTNAME", "VC", "UNSCHEDULABLE", "REPAIR", "GPU ALLOCATED/COUNT", "CPU ALLOCATED/COUNT", "MEMORY ALLOCATED/COUNT", "POD COUNT"},
		[][]string{{
			details.Hostname,
			emptyDash(details.VClusterName),
			fmt.Sprintf("%t", details.Unschedulable),
			fmt.Sprintf("%t", details.Repair),
			details.GPUUsage,
			details.CPUUsage,
			details.MemoryUsage,
			fmt.Sprintf("%d", details.MatchedPodCount),
		}},
		[]int{24, 24, 13, 6, 20, 20, 24, 10},
	)

	fmt.Fprintln(os.Stdout)
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

func PrintJobDetail(result *service.JobGetResult, debugTiming bool) {
	summaryRows := [][]string{
		{"JOB", result.Name},
		{"VC", result.VClusterName},
		{"NAMESPACE", result.Namespace},
		{"UID", result.UID},
		{"SUBMITTER", result.Submitter},
		{"PODGROUP", result.PodGroupName},
		{"IMAGE PULL SECRET", joinOrDash(result.ImagePullSecrets)},
		{"INSPECT POD", result.InspectPod},
		{"NODES", joinOrDash(result.Nodes)},
	}
	printBoxTable([]string{"FIELD", "VALUE"}, summaryRows)

	if len(result.PersistentVolumeClaims) > 0 {
		fmt.Fprintln(os.Stdout)
		pvcRows := make([][]string, 0, len(result.PersistentVolumeClaims))
		for _, pvc := range result.PersistentVolumeClaims {
			pvcRows = append(pvcRows, []string{
				emptyDash(pvc.ClaimName),
				emptyDash(pvc.FrontendVolume),
			})
		}
		printBoxTableWithMaxWidths(
			[]string{"VIRTUAL PVC", "AFS"},
			pvcRows,
			[]int{32, 32},
		)
	}

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
	logRows := make([][]string, 0, len(result.RecentLogLines))
	for _, line := range result.RecentLogLines {
		logRows = append(logRows, []string{line})
	}
	printBoxTableWithMaxWidths(
		[]string{"LATEST LOGS"},
		logRows,
		[]int{110},
	)

	if strings.TrimSpace(result.PodGroupName) != "" {
		fmt.Fprintf(os.Stdout, "For PodGroup diagnosis, switch KUBECONFIG to the target vcluster and run: rayctl job get pg %s\n", result.PodGroupName)
	}

	if debugTiming {
		fmt.Fprintln(os.Stdout)
		printBoxTable(
			[]string{"STEP", "DURATION"},
			[][]string{
				{"locate", result.Timings.Locate.String()},
				{"platform job", formatOptionalDuration(result.Timings.PlatformJob)},
				{"platform pods", formatOptionalDuration(result.Timings.PlatformPods)},
				{"platform events", formatOptionalDuration(result.Timings.PlatformEvents)},
				{"platform logs", formatOptionalDuration(result.Timings.PlatformLogs)},
				{"kube job", formatOptionalDuration(result.Timings.KubeJob)},
				{"kube pods", formatOptionalDuration(result.Timings.KubePods)},
				{"kube events", formatOptionalDuration(result.Timings.KubeEvents)},
				{"kube logs", formatOptionalDuration(result.Timings.KubeLogs)},
				{"format", result.Timings.Format.String()},
				{"total", result.Timings.Total.String()},
			},
		)
	}
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
	statusRows := make([][]string, 0, len(result.StatusMessages))
	for _, message := range result.StatusMessages {
		statusRows = append(statusRows, []string{message})
	}
	printBoxTableWithMaxWidths(
		[]string{"PODGROUP STATUS MESSAGE"},
		statusRows,
		[]int{120},
	)
}

func PrintJobCheckDetail(result *service.JobCheckResult) {
	rows := [][]string{
		{"任务", result.Name},
	}

	for _, line := range result.Diagnosis {
		rows = append(rows, []string{"结论", line})
	}

	if strings.TrimSpace(result.Instruction) != "" {
		rows = append(rows, []string{"下一步", result.Instruction})
	}

	for _, pvc := range result.PVCs {
		rows = append(rows, []string{
			"PVC",
			fmt.Sprintf("virtual pvc=%s | afs=%s | %s",
				emptyDash(pvc.ClaimName),
				emptyDash(pvc.FrontendVolume),
				pvc.Message,
			),
		})
	}

	for _, pod := range result.Pods {
		rows = append(rows, []string{
			"Pod",
			fmt.Sprintf("%s | phase=%s | ready=%s | node=%s | reason=%s", pod.Name, pod.Phase, pod.Ready, pod.NodeName, emptyDash(pod.Reason)),
		})
	}

	for _, secret := range result.SecretChecks {
		rows = append(rows, []string{
			"镜像账号密码",
			fmt.Sprintf("%s | 账号=%s | 密码=%s", emptyDash(secret.SecretName), emptyDash(secret.Username), emptyDash(secret.Password)),
		})
		rows = append(rows, []string{
			"镜像密钥结果",
			fmt.Sprintf("%s", emptyDash(secret.Message)),
		})
	}

	for _, item := range result.PodGroupEvidence {
		rows = append(rows, []string{
			"PodGroup 证据",
			fmt.Sprintf("%s | %s | %s", emptyDash(item.Source), emptyDash(item.Status), emptyDash(item.Detail)),
		})
	}

	resultWidth := terminalWidth() - 16
	if resultWidth > 92 {
		resultWidth = 92
	}
	if resultWidth < 48 {
		resultWidth = 48
	}

	printBoxTableWithMaxWidths(
		[]string{"项目", "结果"},
		rows,
		[]int{12, resultWidth},
	)
}

func PrintAFSCheckDetail(result *service.AFSCheckResult) {
	rows := [][]string{
		{
			emptyDash(result.AFSName),
			joinLinesOrDash(result.HostPVCs),
			joinLinesOrDash(result.HostPVs),
		},
	}
	printBoxTableWithMaxWidths(
		[]string{"AFS", "HOST PVC", "HOST PV"},
		rows,
		[]int{24, 28, 28},
	)

	fmt.Fprintln(os.Stdout)
	displayPVCs := result.VirtualPVCs
	if len(displayPVCs) > 20 {
		displayPVCs = displayPVCs[:20]
	}
	pvcRows := make([][]string, 0, maxInt(1, len(displayPVCs)))
	if len(displayPVCs) == 0 {
		pvcRows = append(pvcRows, []string{"-"})
	} else {
		for _, pvc := range displayPVCs {
			pvcRows = append(pvcRows, []string{
				emptyDash(pvc),
			})
		}
	}
	printBoxTableWithMaxWidths(
		[]string{"VIRTUAL PVC"},
		pvcRows,
		[]int{24},
	)
	fmt.Fprintf(os.Stdout, "总共关联的 Virtual PVC 数量: %d\n", len(result.VirtualPVCs))
	if len(result.VirtualPVCs) > len(displayPVCs) {
		fmt.Fprintf(os.Stdout, "默认仅展示前 %d 个 Virtual PVC。\n", len(displayPVCs))
	}
}

func PrintPVCCheckDetail(result *service.PVCCheckResult) {
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	if len(result.Items) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-"})
	} else {
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.PVCName),
				emptyDash(item.AFSName),
				emptyDash(item.Partition),
				joinOrDash(item.JobNames),
			})
		}
	}

	printBoxTableWithMaxWidths(
		[]string{"PVC", "AFS", "分区", "任务"},
		rows,
		[]int{24, 24, 20, 40},
	)
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

	fitWidthsToTerminal(widths, headers)

	fmt.Fprintln(os.Stdout, borderLine("┌", "┬", "┐", widths))
	fmt.Fprintln(os.Stdout, renderRow(headers, widths))
	fmt.Fprintln(os.Stdout, borderLine("├", "┼", "┤", widths))
	for _, row := range rows {
		for _, rendered := range renderWrappedRows(row, widths) {
			fmt.Fprintln(os.Stdout, rendered)
		}
	}
	fmt.Fprintln(os.Stdout, borderLine("└", "┴", "┘", widths))
}

func fitWidthsToTerminal(widths []int, headers []string) {
	if len(widths) == 0 {
		return
	}

	maxTotal := terminalWidth() - 1
	if maxTotal < 40 {
		maxTotal = 40
	}

	currentTotal := tableDisplayWidth(widths)
	if currentTotal <= maxTotal {
		return
	}

	minWidths := make([]int, len(widths))
	for i := range widths {
		minWidth := 8
		if i < len(headers) {
			headerWidth := textWidth(headers[i])
			if headerWidth > minWidth {
				minWidth = headerWidth
			}
		}
		if len(widths) == 1 {
			minWidth = 20
		}
		minWidths[i] = minWidth
	}

	for currentTotal > maxTotal {
		widest := -1
		widestSpare := 0
		for i := range widths {
			spare := widths[i] - minWidths[i]
			if spare > widestSpare {
				widest = i
				widestSpare = spare
			}
		}
		if widest == -1 || widestSpare <= 0 {
			break
		}
		widths[widest]--
		currentTotal = tableDisplayWidth(widths)
	}
}

func tableDisplayWidth(widths []int) int {
	total := 1
	for _, width := range widths {
		total += width + 3
	}
	return total
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

func renderWrappedRows(row []string, widths []int) []string {
	cellLines := make([][]string, len(widths))
	maxLines := 1
	for i, width := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		lines := wrapCell(cell, width)
		cellLines[i] = lines
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}

	rendered := make([]string, 0, maxLines)
	for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
		current := make([]string, len(widths))
		for cellIdx := range widths {
			if lineIdx < len(cellLines[cellIdx]) {
				current[cellIdx] = cellLines[cellIdx][lineIdx]
			}
		}
		rendered = append(rendered, renderRow(current, widths))
	}
	return rendered
}

func wrapCell(value string, width int) []string {
	if width <= 0 {
		return []string{value}
	}
	if value == "" {
		return []string{""}
	}

	paragraphs := strings.Split(value, "\n")
	lines := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimRight(paragraph, " ")
		runes := []rune(paragraph)
		if len(runes) == 0 {
			lines = append(lines, "")
			continue
		}
		for textWidth(string(runes)) > width {
			split := bestWrapPosition(runes, width)
			lines = append(lines, strings.TrimSpace(string(runes[:split])))
			runes = trimLeadingWrapRunes(runes[split:])
		}
		lines = append(lines, string(runes))
	}
	return lines
}

func bestWrapPosition(runes []rune, width int) int {
	if textWidth(string(runes)) <= width {
		return len(runes)
	}

	best := prefixWithinWidth(runes, width)
	for i := best; i > 0; i-- {
		if isWrapBoundary(runes[i-1]) {
			best = i
			break
		}
	}
	if best <= 0 {
		best = width
	}
	return best
}

func trimLeadingWrapRunes(runes []rune) []rune {
	start := 0
	for start < len(runes) {
		if runes[start] != ' ' {
			break
		}
		start++
	}
	return runes[start:]
}

func isWrapBoundary(ch rune) bool {
	switch ch {
	case ' ', ',', '.', ';', ':', '|', '/', '，', '。', '；', '：', '、', '）', ')', ']', '】', '>':
		return true
	default:
		return false
	}
}

func truncateCell(value string, width int) string {
	if width <= 0 || textWidth(value) <= width {
		return value
	}
	if width <= 1 {
		return "."
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}

	runes := []rune(value)
	return string(runes[:prefixWithinWidth(runes, width-3)]) + "..."
}

func formatRepairFlag(v bool) string {
	return fmt.Sprintf("%t", v)
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}

func joinLinesOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, "\n")
}


func formatOptionalDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	return d.String()
}

func terminalWidth() int {
	if value := strings.TrimSpace(os.Getenv("COLUMNS")); value != "" {
		if width, err := strconv.Atoi(value); err == nil && width > 0 {
			return width
		}
	}
	return 120
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
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
	width := 0
	for _, r := range value {
		width += runeDisplayWidth(r)
	}
	return width
}

func prefixWithinWidth(runes []rune, maxWidth int) int {
	if maxWidth <= 0 {
		return 0
	}
	currentWidth := 0
	for i, r := range runes {
		rw := runeDisplayWidth(r)
		if currentWidth+rw > maxWidth {
			return i
		}
		currentWidth += rw
	}
	return len(runes)
}

func runeDisplayWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 32 || (r >= 0x7f && r < 0xa0):
		return 0
	case unicode.In(r,
		unicode.Han,
		unicode.Hangul,
		unicode.Hiragana,
		unicode.Katakana):
		return 2
	case isFullWidthRune(r):
		return 2
	default:
		return 1
	}
}

func isFullWidthRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F:
		return true
	case r >= 0x2329 && r <= 0x232A:
		return true
	case r >= 0x2E80 && r <= 0xA4CF:
		return true
	case r >= 0xAC00 && r <= 0xD7A3:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0xFE10 && r <= 0xFE19:
		return true
	case r >= 0xFE30 && r <= 0xFE6F:
		return true
	case r >= 0xFF00 && r <= 0xFF60:
		return true
	case r >= 0xFFE0 && r <= 0xFFE6:
		return true
	default:
		return false
	}
}
