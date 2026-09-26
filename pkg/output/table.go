package output

import (
	"fmt"
	"golang.org/x/term"
	"os"
	"rayctl/internal/service"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type tableOptions struct {
	noWrapCells map[string]struct{}
	minWidths   []int
	rowDividers bool
}

func cellKey(row int, col int) string {
	return fmt.Sprintf("%d:%d", row, col)
}

func makeNoWrapCells(cells ...[2]int) map[string]struct{} {
	if len(cells) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(cells))
	for _, cell := range cells {
		result[cellKey(cell[0], cell[1])] = struct{}{}
	}
	return result
}

func noWrapCellsForSingleColumn(rowCount int, col int) [][2]int {
	if rowCount <= 0 {
		return nil
	}
	result := make([][2]int, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		result = append(result, [2]int{i, col})
	}
	return result
}

func noWrapCellsForColumns(rowCount int, columns ...int) [][2]int {
	cells := make([][2]int, 0, rowCount*len(columns))
	for _, column := range columns {
		cells = append(cells, noWrapCellsForSingleColumn(rowCount, column)...)
	}
	return cells
}

func PrintJobDetail(result *service.JobGetResult, longOutput bool, debugTiming bool) {
	if captureJSON(result) {
		return
	}
	summaryRows := [][]string{
		{"TYPE", "ECP VCJob"},
		{"JOB", result.Name},
		{"STATUS", emptyDash(result.Status)},
		{"VC", result.VClusterName},
		{"VC NAMESPACE", result.Namespace},
		{"HOST NAMESPACE", emptyDash(result.HostNamespace)},
	}
	if longOutput {
		summaryRows = append(summaryRows, []string{"UID", result.UID})
	}
	summaryRows = append(summaryRows,
		[]string{"SUBMITTER", result.Submitter},
		[]string{"PODGROUP", result.PodGroupName},
		[]string{"IMAGE PULL SECRET", joinOrDash(result.ImagePullSecrets)},
	)
	summaryRows = append(summaryRows, jobResourceSpecRows(result.ResourceSpecs)...)
	summaryRows = append(summaryRows,
		[]string{"CREATED", emptyDash(result.CreatedAt)},
		[]string{"STARTED", emptyDash(result.StartedAt)},
	)
	if result.Terminal {
		summaryRows = append(summaryRows, []string{"ENDED", emptyDash(result.EndedAt)})
	}
	for _, line := range result.Diagnosis {
		summaryRows = append(summaryRows, []string{"结论", emptyDash(line)})
	}
	for _, secret := range result.SecretChecks {
		summaryRows = append(summaryRows, []string{
			"镜像账号密码",
			fmt.Sprintf("%s | 账号=%s | 密码=%s", emptyDash(secret.SecretName), emptyDash(secret.Username), emptyDash(secret.Password)),
		})
		summaryRows = append(summaryRows, []string{
			"镜像密钥结果",
			emptyDash(secret.Message),
		})
	}
	if strings.TrimSpace(result.Instruction) != "" {
		summaryRows = append(summaryRows, []string{"下一步", result.Instruction})
	}
	if !result.Terminal {
		summaryRows = append(summaryRows,
			[]string{"INSPECT POD", result.InspectPod},
			[]string{"NODES", joinOrDash(result.Nodes)},
		)
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		summaryRows,
		nil,
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
				[2]int{4, 1},
				[2]int{5, 1},
				[2]int{6, 1},
				[2]int{7, 1},
			),
		},
	)

	if len(result.PersistentVolumeClaims) > 0 {
		fmt.Fprintln(os.Stdout)
		pvcRows := make([][]string, 0, len(result.PersistentVolumeClaims))
		for _, pvc := range result.PersistentVolumeClaims {
			pvcRows = append(pvcRows, []string{
				emptyDash(pvc.ClaimName),
				emptyDash(pvc.Status),
				emptyDash(pvc.FrontendVolume),
			})
		}
		printBoxTableWithMaxWidths(
			[]string{"VIRTUAL PVC", "STATUS", "AFS/AOSS ENDPOINT"},
			pvcRows,
			[]int{32, 12, 32},
		)
	}

	if !result.Terminal {
		switch strings.ToLower(strings.TrimSpace(result.Stage)) {
		case "scheduling":
			// Keep only the summary and PVC tables when the job has not been scheduled to any pod yet.
		case "startup", "failed":
			fmt.Fprintln(os.Stdout)
			printJobEvidenceTable("POD EVENTS", result.CheckEvidence)
		default:
		}
	}
	if longOutput && shouldShowJobLogs(result) {
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
				{"diagnosis", formatOptionalDuration(result.Timings.Diagnosis)},
				{"enrichment", formatOptionalDuration(result.Timings.Enrichment)},
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

func PrintSSPJobDetail(result *service.SSPJobGetResult, longOutput bool) {
	if captureJSON(result) {
		return
	}
	if result != nil && longOutput {
		defer func() { printPodEvidence(result.PodEvidence) }()
	}
	if result == nil {
		return
	}
	summaryRows := [][]string{
		{"TYPE", "SSP TrainingJob"},
		{"JOB", emptyDash(result.Name)},
		{"STATUS", emptyDash(result.Status)},
		{"VC", emptyDash(result.VCluster)},
		{"WORKSPACE", emptyDash(result.Workspace)},
		{"QUEUE", emptyDash(result.Queue)},
		{"QUEUE TYPE", emptyDash(result.QueueType)},
		{"VC NAMESPACE", emptyDash(result.Namespace)},
		{"HOST NAMESPACE", emptyDash(result.HostNamespace)},
	}
	if longOutput {
		summaryRows = append(summaryRows, []string{"UID", emptyDash(result.UID)})
	}
	summaryRows = append(summaryRows,
		[]string{"SUBMITTER", emptyDash(result.Submitter)},
		[]string{"FRAMEWORK", emptyDash(result.Framework)},
		[]string{"PRIORITY", emptyDash(result.Priority)},
		[]string{"REQUIRED NODES", strconv.Itoa(result.RequiredNodes)},
	)
	summaryRows = append(summaryRows, jobResourceSpecRows(result.ResourceSpecs)...)
	summaryRows = append(summaryRows,
		[]string{"CREATED", emptyDash(result.CreatedAt)},
		[]string{"STARTED", emptyDash(result.StartedAt)},
	)
	if longOutput {
		summaryRows = append(summaryRows, []string{"IMAGE PULL SECRET", joinOrDash(result.ImagePullSecrets)})
	}
	if result.Terminal {
		summaryRows = append(summaryRows, []string{"ENDED", emptyDash(result.EndedAt)})
	}
	for _, line := range result.Diagnosis {
		summaryRows = append(summaryRows, []string{"结论", emptyDash(line)})
	}
	for _, secret := range result.SecretChecks {
		summaryRows = append(summaryRows,
			[]string{"镜像账号密码", fmt.Sprintf("%s | 账号=%s | 密码=%s", emptyDash(secret.SecretName), emptyDash(secret.Username), emptyDash(secret.Password))},
			[]string{"镜像密钥结果", emptyDash(secret.Message)},
		)
	}
	if strings.TrimSpace(result.Instruction) != "" {
		summaryRows = append(summaryRows, []string{"下一步", result.Instruction})
	}
	if !result.Terminal {
		summaryRows = append(summaryRows,
			[]string{"INSPECT POD", emptyDash(result.InspectPod)},
			[]string{"NODES", joinOrDash(result.Nodes)},
		)
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		summaryRows,
		nil,
		tableOptions{noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(12, 1)...)},
	)

	if longOutput && len(result.PodResources) > 0 {
		fmt.Fprintln(os.Stdout)
		rows := make([][]string, 0, len(result.PodResources))
		for _, pod := range result.PodResources {
			rows = append(rows, []string{
				emptyDash(pod.Pod),
				emptyDash(pod.Phase),
				emptyDash(pod.Node),
				emptyDash(pod.CPU),
				emptyDash(pod.Memory),
				emptyDash(pod.MachineType),
				emptyDash(pod.Model),
				emptyDash(pod.Accelerator),
			})
		}
		printBoxTableWithMaxWidths(
			[]string{"POD", "PHASE", "NODE", "CPU", "MEMORY", "MACHINE TYPE", "MODEL", "ACCELERATOR"},
			rows,
			[]int{52, 12, 26, 10, 12, 28, 24, 12},
		)
	}

	if longOutput && len(result.PersistentVolumeClaims) > 0 {
		fmt.Fprintln(os.Stdout)
		rows := make([][]string, 0, len(result.PersistentVolumeClaims))
		for _, pvc := range result.PersistentVolumeClaims {
			rows = append(rows, []string{
				emptyDash(pvc.ClaimName),
				emptyDash(pvc.MountPath),
				emptyDash(pvc.VolumeType),
				emptyDash(pvc.Status),
				emptyDash(pvc.FrontendVolume),
			})
		}
		printBoxTableWithMaxWidths(
			[]string{"PVC", "MOUNT PATH", "TYPE", "STATUS", "AFS/AOSS ENDPOINT"},
			rows,
			[]int{36, 28, 8, 12, 40},
		)
	}

	if !result.Terminal && len(result.CheckEvidence) > 0 {
		fmt.Fprintln(os.Stdout)
		printJobEvidenceTable("DIAGNOSIS", result.CheckEvidence)
	}
	if longOutput && !result.Terminal && strings.EqualFold(result.Stage, "running") && len(result.RecentLogLines) > 0 {
		fmt.Fprintln(os.Stdout)
		rows := make([][]string, 0, len(result.RecentLogLines))
		for _, line := range result.RecentLogLines {
			rows = append(rows, []string{line})
		}
		printBoxTableWithMaxWidths([]string{"LATEST LOGS"}, rows, []int{110})
	}
}

type groupedJobResourceSpec struct {
	spec  service.JobResourceSpecItem
	tasks []string
}

func jobResourceSpecRows(specs []service.JobResourceSpecItem) [][]string {
	groups := make([]groupedJobResourceSpec, 0, len(specs))
	groupIndex := make(map[string]int)
	for _, spec := range specs {
		key := strings.Join([]string{
			strings.TrimSpace(spec.CPU), strings.TrimSpace(spec.Memory), strings.TrimSpace(spec.Accelerator),
			strings.TrimSpace(spec.AcceleratorResource), strings.TrimSpace(spec.RDMA), strings.TrimSpace(spec.RDMAResource),
			strings.TrimSpace(spec.Model), strings.TrimSpace(spec.MachineType),
		}, "\x00")
		if strings.Trim(key, "\x00") == "" {
			continue
		}
		index, exists := groupIndex[key]
		if !exists {
			index = len(groups)
			groupIndex[key] = index
			groups = append(groups, groupedJobResourceSpec{spec: spec})
		}
		task := strings.TrimSpace(spec.Task)
		if task != "" {
			if spec.Replicas > 0 {
				task = fmt.Sprintf("%s×%d", task, spec.Replicas)
			}
			groups[index].tasks = append(groups[index].tasks, task)
		}
	}
	rows := make([][]string, 0, len(groups))
	for _, group := range groups {
		field := "SPEC / NODE"
		if len(groups) > 1 && len(group.tasks) > 0 {
			field = "SPEC " + strings.Join(group.tasks, ",")
		}
		rows = append(rows, []string{field, formatJobResourceSpec(group.spec)})
	}
	return rows
}

func formatJobResourceSpec(spec service.JobResourceSpecItem) string {
	parts := make([]string, 0, 4)
	if value := strings.TrimSpace(spec.CPU); value != "" {
		parts = append(parts, value+" CPU")
	}
	if value := strings.TrimSpace(spec.Memory); value != "" {
		parts = append(parts, value+" Memory")
	}
	accelerator := strings.TrimSpace(spec.Accelerator)
	model := strings.TrimSpace(spec.Model)
	if accelerator != "" {
		if model != "" {
			accelerator += " " + model
		} else if resourceName := shortAcceleratorResource(spec.AcceleratorResource); resourceName != "" {
			accelerator += " " + resourceName
		}
		parts = append(parts, accelerator)
	} else if model != "" {
		parts = append(parts, model)
	}
	if rdma := formatRDMAResource(spec.RDMA, spec.RDMAResource); rdma != "" {
		parts = append(parts, rdma)
	}
	if value := strings.TrimSpace(spec.MachineType); value != "" {
		parts = append(parts, value+" Machine Type")
	}
	return emptyDash(strings.Join(parts, " / "))
}

func formatRDMAResource(quantity, resourceName string) string {
	quantity = strings.TrimSpace(quantity)
	resourceName = shortAcceleratorResource(resourceName)
	if quantity == "" && resourceName == "" {
		return ""
	}
	value := "RDMA"
	if quantity != "" {
		value = quantity + " " + value
	}
	if resourceName != "" {
		value += " (" + resourceName + ")"
	}
	return value
}

func shortAcceleratorResource(value string) string {
	parts := strings.Split(strings.TrimSpace(value), "+")
	for index := range parts {
		if slash := strings.LastIndex(parts[index], "/"); slash >= 0 && slash+1 < len(parts[index]) {
			parts[index] = parts[index][slash+1:]
		}
	}
	return strings.Join(parts, "+")
}

func PrintSSPAIDDetail(result *service.SSPAIDGetResult, longOutput bool, debugTiming bool) {
	if captureJSON(result) {
		return
	}
	if result != nil && longOutput {
		defer func() { printPodEvidence(result.PodEvidence) }()
	}
	if result == nil {
		return
	}
	rows := [][]string{
		{"AID", emptyDash(result.Name)},
		{"STATE", emptyDash(result.State)},
		{"VC", emptyDash(result.VCluster)},
		{"WORKSPACE", emptyDash(result.Workspace)},
		{"VC NAMESPACE", emptyDash(result.Namespace)},
		{"HOST NAMESPACE", emptyDash(result.HostNamespace)},
		{"QUEUE", emptyDash(result.Queue)},
		{"QUEUE TYPE", emptyDash(result.QueueType)},
		{"PRIORITY", emptyDash(result.Priority)},
		{"SUBMITTER", emptyDash(result.Submitter)},
		{"INTERNAL IP", emptyDash(result.InternalIP)},
		{"SSH", emptyDash(result.SSHEnabled)},
		{"CODE SERVER", emptyDash(result.CodeServerEnabled)},
		{"IMAGE", emptyDash(result.Image)},
		{"IMAGE TYPE", emptyDash(result.ImageType)},
		{"RESOURCE", emptyDash(result.ResourceSummary)},
		{"CREATED", emptyDash(result.CreatedAt)},
	}
	if longOutput {
		rows = append(rows, []string{"UID", emptyDash(result.UID)})
	}
	for _, line := range result.Diagnosis {
		rows = append(rows, []string{"结论", emptyDash(line)})
	}
	for _, secret := range result.SecretChecks {
		rows = append(rows,
			[]string{"镜像账号密码", fmt.Sprintf("%s | 账号=%s | 密码=%s", emptyDash(secret.SecretName), emptyDash(secret.Username), emptyDash(secret.Password))},
			[]string{"镜像密钥结果", emptyDash(secret.Message)},
		)
	}
	if result.Instruction != "" {
		rows = append(rows, []string{"下一步", result.Instruction})
	}
	if !result.Terminal {
		rows = append(rows,
			[]string{"INSPECT POD", emptyDash(result.InspectPod)},
			[]string{"NODES", joinOrDash(result.Nodes)},
		)
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{20, 100},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1}, [2]int{1, 1}, [2]int{2, 1}, [2]int{3, 1},
				[2]int{4, 1}, [2]int{5, 1}, [2]int{6, 1}, [2]int{7, 1},
				[2]int{8, 1}, [2]int{9, 1}, [2]int{10, 1}, [2]int{12, 1}, [2]int{13, 1},
			),
			minWidths: []int{16, 28},
		},
	)

	if len(result.DNATRules) > 0 {
		fmt.Fprintln(os.Stdout)
		dnatRows := make([][]string, 0, len(result.DNATRules))
		for _, rule := range result.DNATRules {
			dnatRows = append(dnatRows, []string{
				emptyDash(rule.External),
				emptyDash(rule.Internal),
				emptyDash(rule.Protocol),
				emptyDash(rule.State),
			})
		}
		printBoxTableWithMaxWidths(
			[]string{"EXTERNAL", "INTERNAL", "PROTOCOL", "STATE"},
			dnatRows,
			[]int{28, 28, 12, 14},
		)
	}

	if longOutput && len(result.PersistentVolumeClaims) > 0 {
		fmt.Fprintln(os.Stdout)
		pvcRows := make([][]string, 0, len(result.PersistentVolumeClaims))
		for _, pvc := range result.PersistentVolumeClaims {
			pvcRows = append(pvcRows, []string{emptyDash(pvc.ClaimName), emptyDash(pvc.Status), emptyDash(pvc.FrontendVolume)})
		}
		printBoxTableWithMaxWidths([]string{"PVC", "STATUS", "AFS/AOSS ENDPOINT"}, pvcRows, []int{38, 12, 38})
	}

	if !result.Terminal && len(result.CheckEvidence) > 0 {
		fmt.Fprintln(os.Stdout)
		printJobEvidenceTable("DIAGNOSIS", result.CheckEvidence)
	}
	if longOutput && !result.Terminal && strings.EqualFold(result.Stage, "running") && len(result.RecentLogLines) > 0 {
		fmt.Fprintln(os.Stdout)
		logRows := make([][]string, 0, len(result.RecentLogLines))
		for _, line := range result.RecentLogLines {
			logRows = append(logRows, []string{line})
		}
		printBoxTableWithMaxWidths([]string{"LATEST LOGS"}, logRows, []int{110})
	}
	if debugTiming {
		fmt.Fprintln(os.Stdout)
		printBoxTableWithMaxWidths(
			[]string{"STEP", "DURATION"},
			[][]string{
				{"locate pods", result.Timings.LocatePods.String()},
				{"resolve region", result.Timings.ResolveRegion.String()},
				{"workspace", result.Timings.Workspace.String()},
				{"subscription", result.Timings.Subscription.String()},
				{"platform aid", result.Timings.PlatformAID.String()},
				{"refine pods", result.Timings.RefinePods.String()},
				{"build detail", result.Timings.BuildDetail.String()},
				{"dnat", result.Timings.DNAT.String()},
				{"total", result.Timings.Total.String()},
			},
			[]int{18, 18},
		)
	}
}

func PrintJobClusterList(result *service.JobClusterListResult) {
	if captureJSON(result) {
		return
	}
	totalJobs := fmt.Sprintf("%d", result.TotalJobCount)
	if result.TotalJobCount < 0 {
		totalJobs = "-（全局快速模式未统计）"
	}
	totalPods := fmt.Sprintf("%d", result.TotalPodCount)
	if result.TotalPodCount < 0 {
		totalPods = "-（默认未拉取历史 Pod）"
	}
	summaryRows := [][]string{
		{"分区名", emptyDash(result.ClusterName)},
		{"活跃任务数量", fmt.Sprintf("%d", result.ActiveJobCount)},
		{"活跃 Pod 数量", fmt.Sprintf("%d", result.ActivePodCount)},
		{"总任务数量", totalJobs},
		{"总 Pod 数量", totalPods},
	}
	if strings.TrimSpace(result.StatusFilter) != "" {
		summaryRows = append(summaryRows, []string{"过滤条件", strings.TrimSpace(result.StatusFilter)})
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		summaryRows,
		[]int{18, 48},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(summaryRows), 1)...),
			minWidths:   []int{12, 20},
		},
	)

	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, len(result.Items))
	for _, item := range result.Items {
		row := []string{}
		if result.ShowClusterName {
			row = append(row, emptyDash(item.ClusterName))
		}
		createdAt := item.CreatedAt
		if result.ShowClusterName {
			createdAt = item.CreatedAtShort
		}
		row = append(row,
			emptyDash(item.JobName),
			emptyDash(item.Submitter),
			emptyDash(item.Status),
			emptyDash(createdAt),
		)
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		row := []string{}
		if result.ShowClusterName {
			row = append(row, "-")
		}
		row = append(row, "-", "-", "-", "-")
		rows = append(rows, row)
	}

	headers := []string{"任务", "提交人", "状态", "创建时间"}
	maxWidths := []int{56, 18, 10, 19}
	minWidths := []int{18, 8, 8, 19}
	combinedNoWrap := make([][2]int, 0)
	combinedNoWrap = append(combinedNoWrap, noWrapCellsForSingleColumn(len(rows), 1)...)
	combinedNoWrap = append(combinedNoWrap, noWrapCellsForSingleColumn(len(rows), 2)...)
	combinedNoWrap = append(combinedNoWrap, noWrapCellsForSingleColumn(len(rows), 3)...)
	if result.ShowClusterName {
		headers = append([]string{"分区名"}, headers...)
		maxWidths = []int{24, 64, 32, 7, 11}
		minWidths = []int{12, 20, 10, 7, 11}
		combinedNoWrap = make([][2]int, 0)
		combinedNoWrap = append(combinedNoWrap, noWrapCellsForSingleColumn(len(rows), 3)...)
		combinedNoWrap = append(combinedNoWrap, noWrapCellsForSingleColumn(len(rows), 4)...)
	}

	printBoxTableWithOptions(
		headers,
		rows,
		maxWidths,
		tableOptions{
			noWrapCells: makeNoWrapCells(combinedNoWrap...),
			minWidths:   minWidths,
		},
	)
}

func PrintECPJobList(result *service.JobClusterListResult) {
	if captureJSON(result) {
		return
	}
	rows := make([][]string, 0)
	total := 0
	if result != nil {
		total = len(result.Items)
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.JobName),
				emptyDash(item.Status),
				emptyDash(item.ClusterName),
				emptyDash(item.Submitter),
				emptyDash(item.CreatedAt),
			})
		}
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"NAME", "STATE", "VC", "CREATOR", "CREATED"},
		rows,
		[]int{56, 16, 28, 22, 20},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), 0, 1, 2, 3, 4)...),
			minWidths:   []int{28, 12, 18, 14, 19},
		},
	)
	fmt.Fprintf(os.Stdout, "\n本次筛选共 %d 条。\n", total)
	if result != nil && len(result.Warnings) > 0 {
		const warningLimit = 5
		for index, warning := range result.Warnings {
			if index >= warningLimit {
				fmt.Fprintf(os.Stdout, "Warning: 另有 %d 个 VC 查询失败。\n", len(result.Warnings)-warningLimit)
				break
			}
			fmt.Fprintf(os.Stdout, "Warning: %s\n", warning)
		}
	}
}

func PrintVPCList(result *service.VPCListResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.CIDR), emptyDash(item.RDMA),
			strconv.Itoa(item.SubnetCount), emptyDash(item.NATGateways), emptyDash(item.Region),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"VPC", "STATE", "CIDR", "RDMA", "SUBNETS", "NATGW", "REGION"},
		rows,
		[]int{32, 10, 20, 6, 8, 28, 12},
		tableOptions{minWidths: []int{16, 8, 12, 4, 7, 10, 8}},
	)
}

func PrintVPCDetail(result *service.VPCListItem) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	printResourceDetail([][]string{
		{"VPC", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"CIDR", emptyDash(result.CIDR)},
		{"RDMA", emptyDash(result.RDMA)},
		{"DEFAULT", emptyDash(result.Default)},
		{"SUBNET COUNT", strconv.Itoa(result.SubnetCount)},
		{"NAT GATEWAY", emptyDash(result.NATGateways)},
		{"REGION", emptyDash(result.Region)},
		{"CREATED", emptyDash(result.CreatedAt)},
		{"UPDATED", emptyDash(result.UpdatedAt)},
	})
}

func PrintSubnetList(result *service.SubnetListResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.VPC), emptyDash(item.CIDR),
			emptyDash(item.AvailableIPs), emptyDash(item.Zone),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"SUBNET", "STATE", "VPC", "CIDR", "AVAILABLE", "ZONE"},
		rows,
		[]int{32, 10, 28, 20, 10, 12},
		tableOptions{minWidths: []int{18, 8, 14, 12, 9, 8}},
	)
}

func PrintSubnetDetail(result *service.SubnetListItem) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	printResourceDetail([][]string{
		{"SUBNET", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"VPC", emptyDash(result.VPC)},
		{"CIDR", emptyDash(result.CIDR)},
		{"GATEWAY", emptyDash(result.Gateway)},
		{"AVAILABLE IPS", emptyDash(result.AvailableIPs)},
		{"SCOPE", emptyDash(result.Scope)},
		{"PROVIDER", emptyDash(result.Provider)},
		{"NETWORK TYPE", emptyDash(result.NetworkType)},
		{"ZONE", emptyDash(result.Zone)},
		{"CREATED", emptyDash(result.CreatedAt)},
		{"UPDATED", emptyDash(result.UpdatedAt)},
	})
}

func PrintNATGatewayList(result *service.NATGatewayListResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.VPC), emptyDash(item.Subnet),
			emptyDash(item.GatewayType), strconv.Itoa(item.EIPCount), strconv.Itoa(item.SNATCount), strconv.Itoa(item.DNATCount),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"NATGW", "STATE", "VPC", "SUBNET", "TYPE", "EIPS", "SNAT", "DNAT"},
		rows,
		[]int{30, 10, 26, 26, 10, 6, 6, 6},
		tableOptions{minWidths: []int{16, 8, 12, 12, 6, 5, 5, 5}},
	)
}

func PrintNATGatewayDetail(result *service.NATGatewayListItem) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	printResourceDetail([][]string{
		{"NAT GATEWAY", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"VPC", emptyDash(result.VPC)},
		{"SUBNET", emptyDash(result.Subnet)},
		{"TYPE", emptyDash(result.GatewayType)},
		{"EIP", emptyDash(result.EIPs)},
		{"SNAT RULES", strconv.Itoa(result.SNATCount)},
		{"DNAT RULES", strconv.Itoa(result.DNATCount)},
		{"ZONE", emptyDash(result.Zone)},
		{"CREATED", emptyDash(result.CreatedAt)},
		{"UPDATED", emptyDash(result.UpdatedAt)},
	})
}

func PrintAFSList(result *service.AFSListResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Name), emptyDash(item.State), emptyDash(item.Capacity), emptyDash(item.StorageClass),
			emptyDash(item.Zone), emptyDash(item.CreatedAt),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"AFS", "STATE", "CAPACITY", "STORAGE CLASS", "ZONE", "CREATED"},
		rows,
		[]int{36, 10, 12, 18, 12, 20},
		tableOptions{minWidths: []int{18, 8, 8, 12, 8, 16}},
	)
}

func PrintAFSDetail(result *service.AFSListItem) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	printResourceDetail([][]string{
		{"AFS", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"CAPACITY", emptyDash(result.Capacity)},
		{"STORAGE CLASS", emptyDash(result.StorageClass)},
		{"ZONE", emptyDash(result.Zone)},
		{"REGION", emptyDash(result.Region)},
		{"CREATED", emptyDash(result.CreatedAt)},
		{"UPDATED", emptyDash(result.UpdatedAt)},
		{"RID", emptyDash(result.RID)},
	})
}

func PrintAFSCreatePlan(plan *service.AFSCreatePlan, showPayload bool) {
	if captureJSON(plan) {
		return
	}
	if plan == nil {
		return
	}
	printResourceDetail([][]string{
		{"AFS", emptyDash(plan.Name)},
		{"SIZE", emptyDash(plan.Size)},
		{"STORAGE CLASS", emptyDash(plan.StorageClass)},
		{"ZONE", emptyDash(plan.Zone)},
		{"PROFILE", emptyDash(plan.Profile)},
		{"BILLING", fmt.Sprintf("%d years, auto-renew=%t", plan.Years, plan.AutoRenew)},
		{"SKU", emptyDash(firstNonEmptyOutput(plan.SKUName, plan.SKU))},
	})
	if !showPayload {
		return
	}
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions([]string{"REQUEST URL"}, [][]string{{emptyDash(plan.RequestURL)}}, []int{140}, tableOptions{minWidths: []int{40}})
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions([]string{"PAYLOAD"}, [][]string{{emptyDash(plan.Payload)}}, []int{120}, tableOptions{minWidths: []int{40}})
}

func PrintAFSCreateResult(result *service.AFSCreateResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	fmt.Fprintln(os.Stdout)
	rows := [][]string{
		{"AFS", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"ORDER ID", emptyDash(result.OrderID)},
		{"ORDER STATE", emptyDash(result.OrderState)},
		{"AMOUNT", emptyDash(result.Amount)},
		{"PAYMENT", emptyDash(result.PaymentResult)},
		{"PROFILE", emptyDash(result.Profile)},
	}
	if strings.TrimSpace(result.Warning) != "" {
		rows = append(rows, []string{"WARNING", result.Warning})
	}
	printResourceDetail(rows)
}

func printResourceDetail(rows [][]string) {
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{18, 100},
		tableOptions{minWidths: []int{12, 24}},
	)
}

func PrintECPWorkloadLogs(result *service.ECPWorkloadLogResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}

	summaryRows := [][]string{
		{"VC", emptyDash(result.VCluster)},
		{"PROFILE", emptyDash(result.ProfileName)},
		{"TYPE", emptyDash(result.WorkloadType)},
		{"NAME", emptyDash(result.WorkloadName)},
		{"SINCE", formatOptionalDuration(result.Since)},
		{"COUNT", strconv.Itoa(len(result.Items))},
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		summaryRows,
		[]int{12, 72},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
				[2]int{4, 1},
				[2]int{5, 1},
			),
			minWidths: []int{8, 24},
		},
	)

	rows := make([][]string, 0, maxInt(len(result.Items), 1))
	noWrapCells := make([][2]int, 0, len(result.Items)*5)
	if len(result.Items) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "no logs"})
	} else {
		for rowIdx, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.Time),
				emptyDash(item.Level),
				emptyDash(item.WorkloadName),
				emptyDash(item.Pod),
				emptyDash(item.Container),
				emptyDash(item.Message),
			})
			for _, colIdx := range []int{0, 1, 2, 3, 4} {
				noWrapCells = append(noWrapCells, [2]int{rowIdx, colIdx})
			}
		}
	}

	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions(
		[]string{"TIME", "LEVEL", "WORKLOAD", "POD", "CONTAINER", "MESSAGE"},
		rows,
		[]int{19, 5, 36, 48, 24, 120},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{19, 5, 18, 18, 12, 24},
		},
	)
}

func PrintCloudAuditLogs(result *service.CloudAuditLogResult, long bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}

	rows := make([][]string, 0, maxInt(len(result.Items), 1))
	columnCount := 6
	if long {
		columnCount++
	}
	noWrapCells := make([][2]int, 0, len(result.Items)*columnCount)
	if len(result.Items) == 0 {
		row := []string{"-", "-", "-", "-", "-", "no audit logs"}
		if long {
			row = append(row, "-")
		}
		rows = append(rows, row)
	} else {
		for rowIdx, item := range result.Items {
			row := []string{
				emptyDash(item.Time),
				emptyDash(item.ServiceType),
				emptyDash(item.ResourceType),
				emptyDash(item.ResourceName),
				emptyDash(item.OperationType),
				cloudAuditUserName(item.UserName, long),
			}
			if long {
				row = append(row, emptyDash(item.UserID))
			}
			rows = append(rows, row)
			for colIdx := range rows[len(rows)-1] {
				noWrapCells = append(noWrapCells, [2]int{rowIdx, colIdx})
			}
		}
	}

	headers := []string{"TIME", "服务类型", "资源类型", "RESOURCE NAME", "OPERATION TYPE", "USER"}
	maxWidths := []int{19, 8, 40, 64, 24, 48}
	minWidths := []int{19, 8, 16, 20, 12, 12}
	if long {
		maxWidths[5] = 120
		headers = append(headers, "USER ID")
		maxWidths = append(maxWidths, 40)
		minWidths = append(minWidths, 24)
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
}

func cloudAuditUserName(value string, long bool) string {
	value = emptyDash(value)
	if long || value == "-" {
		return value
	}
	const serviceAccountPrefix = "system:serviceaccount:"
	if !strings.HasPrefix(value, serviceAccountPrefix) {
		return value
	}
	if index := strings.LastIndex(value, ":"); index >= 0 && index+1 < len(value) {
		return value[index+1:]
	}
	return value
}

func PrintPVCheckDetail(result *service.PVCheckResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}

	storageType := strings.ToUpper(strings.TrimSpace(result.StorageType))
	if storageType == "" {
		storageType = "AFS"
	}
	rows := [][]string{
		{"HOST PV", emptyDash(result.HostPVName)},
		{"HOST PVC", emptyDash(result.HostPVCName)},
		{storageType, emptyDash(result.AFSName)},
	}
	if strings.TrimSpace(result.Tenant) != "" && strings.TrimSpace(result.Tenant) != "-" {
		rows = append(rows, []string{"TENANT", emptyDash(result.Tenant)})
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{12, 88},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 1)...),
			minWidths:   []int{10, 28},
		},
	)
}

func printJobEvidenceTable(title string, evidence []service.CheckEvidenceItem) {
	rows := make([][]string, 0, maxInt(1, len(evidence)))
	if len(evidence) == 0 {
		rows = append(rows, []string{"-", "-", "no events"})
	} else {
		for _, item := range evidence {
			rows = append(rows, []string{
				emptyDash(item.Source),
				emptyDash(item.Status),
				emptyDash(item.Detail),
			})
		}
	}
	printBoxTableWithMaxWidths(
		[]string{title + " SOURCE", "STATUS", "DETAIL"},
		rows,
		[]int{28, 18, 110},
	)
}

func shouldShowJobLogs(result *service.JobGetResult) bool {
	if result == nil || len(result.RecentLogLines) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(result.Stage)) {
	case "scheduling", "startup":
		return false
	default:
		return true
	}
}

func PrintPodGroupDetail(result *service.PodGroupGetResult) {
	if captureJSON(result) {
		return
	}
	summaryRows := [][]string{
		{"PODGROUP", result.Name},
		{"NAMESPACE", result.Namespace},
		{"STATUS", result.Status},
		{"MIN MEMBER", result.MinMember},
		{"QUEUE", result.Queue},
		{"CREATED AT", result.CreatedAt},
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		summaryRows,
		nil,
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
				[2]int{4, 1},
				[2]int{5, 1},
			),
		},
	)

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
	if captureJSON(result) {
		return
	}
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
			fmt.Sprintf("virtual pvc=%s | afs/aoss endpoint=%s | %s",
				emptyDash(pvc.ClaimName),
				emptyDash(pvc.FrontendVolume),
				pvc.Message,
			),
		})
	}

	for _, pod := range result.Pods {
		rows = append(rows, []string{
			"Pod 状况",
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

	for _, item := range result.PodEvidence {
		rows = append(rows, []string{
			"Pod 事件",
			fmt.Sprintf("%s | %s | %s", emptyDash(item.Source), emptyDash(item.Status), emptyDash(item.Detail)),
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

	printBoxTableWithOptions(
		[]string{"项目", "结果"},
		rows,
		[]int{12, resultWidth},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
			),
		},
	)
}

func PrintAFSCheckDetail(result *service.AFSCheckResult, longOutput bool) {
	if captureJSON(result) {
		return
	}
	rows := [][]string{
		{"AFS", emptyDash(result.AFSName)},
		{"UID", emptyDash(result.UID)},
		{"STATE", emptyDash(result.State)},
		{"CAPACITY", emptyDash(result.Capacity)},
		{"STORAGE CLASS", emptyDash(result.StorageClass)},
		{"ZONE", emptyDash(result.Zone)},
	}
	if longOutput {
		rows = append(rows,
			[]string{"租户", emptyDash(result.Tenant)},
			[]string{"HOST PVC", joinOrDash(result.HostPVCs)},
			[]string{"HOST PV", joinOrDash(result.HostPVs)},
		)
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{16, 120},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 1)...),
		},
	)

	if !longOutput {
		return
	}
	fmt.Fprintln(os.Stdout)
	displayPVCs := result.VirtualPVCs
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
	fmt.Fprintf(os.Stdout, "最多展示 15 个关联 Virtual PVC，每个资源 namespace 仅保留一个。\n")
}

func PrintAFSCheckSummary(results []*service.AFSCheckResult) {
	if captureJSON(results) {
		return
	}
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		rows = append(rows, []string{
			emptyDash(result.AFSName), emptyDash(result.State), emptyDash(result.Capacity),
			emptyDash(result.StorageClass),
		})
	}
	printBoxTableWithOptions(
		[]string{"AFS", "STATE", "CAPACITY", "STORAGE CLASS"},
		rows,
		[]int{32, 10, 12, 18},
		tableOptions{minWidths: []int{16, 8, 8, 12}},
	)
}

func PrintPVCCheckDetail(result *service.PVCCheckResult, longOutput bool) {
	if captureJSON(result) {
		return
	}
	if len(result.Items) == 0 {
		rows := [][]string{
			{"PVC", "-"},
			{"AFS/AOSS ENDPOINT", "-"},
			{"分区", "-"},
			{"任务", "-"},
		}
		if longOutput {
			rows = append(rows, []string{"租户", "-"})
		}
		printBoxTableWithOptions(
			[]string{"FIELD", "VALUE"},
			rows,
			[]int{20, 120},
			tableOptions{
				noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 1)...),
			},
		)
		fmt.Fprintln(os.Stdout, "提醒: 查询 PVC 建议使用 HC kubeconfig；VC kubeconfig 下通常无法完整反查 AFS/AOSS、分区和任务。")
		return
	}

	for i, item := range result.Items {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		rows := [][]string{
			{"PVC", emptyDash(item.PVCName)},
			{"AFS/AOSS ENDPOINT", emptyDash(item.AFSName)},
			{"分区", emptyDash(item.Partition)},
			{"任务", joinOrDash(item.JobNames)},
		}
		if longOutput {
			rows = append(rows, []string{"租户", emptyDash(item.Tenant)})
		}
		printBoxTableWithOptions(
			[]string{"FIELD", "VALUE"},
			rows,
			[]int{20, 120},
			tableOptions{
				noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 1)...),
			},
		)
	}
	fmt.Fprintln(os.Stdout, "提醒: 查询 PVC 建议使用 HC kubeconfig；VC kubeconfig 下通常无法完整反查 AFS/AOSS、分区和任务。")
}

func PrintECSCheckDetail(result *service.ECSCheckResult) {
	if captureJSON(result) {
		return
	}
	if result == nil || len(result.Items) == 0 {
		printBoxTableWithOptions(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"类型", "-"},
				{"名称", "-"},
				{"UID", "-"},
				{"VM", "-"},
				{"Namespace", "-"},
				{"Node", "-"},
				{"创建人", "-"},
				{"内网 IP", "-"},
			},
			[]int{16, 120},
			tableOptions{
				noWrapCells: makeNoWrapCells(
					[2]int{0, 1},
					[2]int{1, 1},
					[2]int{2, 1},
					[2]int{3, 1},
					[2]int{4, 1},
					[2]int{5, 1},
					[2]int{6, 1},
					[2]int{7, 1},
				),
			},
		)
		fmt.Fprintln(os.Stdout)
		printBoxTableWithOptions(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"状态", "-"},
				{"机器规格", "-"},
				{"类型", "-"},
				{"镜像名称", "-"},
				{"VPC", "-"},
			},
			[]int{16, 120},
			tableOptions{
				noWrapCells: makeNoWrapCells(
					[2]int{0, 1},
					[2]int{1, 1},
					[2]int{2, 1},
					[2]int{3, 1},
					[2]int{4, 1},
				),
			},
		)
		return
	}

	for i, item := range result.Items {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		printBoxTableWithOptions(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"类型", emptyDash(item.ResourceType)},
				{"名称", emptyDash(item.Name)},
				{"UID", emptyDash(item.UID)},
				{"VM", emptyDash(item.VMName)},
				{"Namespace", emptyDash(item.Namespace)},
				{"Node", emptyDash(item.Node)},
				{"创建人", emptyDash(item.Creator)},
				{"内网 IP", emptyDash(item.InternalIP)},
			},
			[]int{16, 120},
			tableOptions{
				noWrapCells: makeNoWrapCells(
					[2]int{0, 1},
					[2]int{1, 1},
					[2]int{2, 1},
					[2]int{3, 1},
					[2]int{4, 1},
					[2]int{5, 1},
					[2]int{6, 1},
					[2]int{7, 1},
				),
			},
		)
		fmt.Fprintln(os.Stdout)
		printBoxTableWithOptions(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"状态", emptyDash(item.State)},
				{"机器规格", emptyDash(item.MachineType)},
				{"类型", emptyDash(item.Type)},
				{"镜像名称", emptyDash(item.ImageName)},
				{"VPC", emptyDash(item.VPC)},
			},
			[]int{16, 120},
			tableOptions{
				noWrapCells: makeNoWrapCells(
					[2]int{0, 1},
					[2]int{1, 1},
					[2]int{2, 1},
					[2]int{3, 1},
					[2]int{4, 1},
				),
			},
		)
	}
}

func PrintJobCreatePreview(rows [][]string) {
	printBoxTableWithOptions(
		[]string{"项目", "值"},
		rows,
		[]int{18, 120},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
				[2]int{4, 1},
				[2]int{5, 1},
				[2]int{6, 1},
				[2]int{7, 1},
				[2]int{8, 1},
				[2]int{9, 1},
				[2]int{10, 1},
			),
		},
	)
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func yesNo(value bool) string {
	if value {
		return "Y"
	}
	return "N"
}

func yesNoFromReady(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ready", "true", "yes", "y":
		return "Y"
	default:
		return "N"
	}
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
	printBoxTableWithOptions(headers, rows, nil, tableOptions{})
}

func printBoxTableWithMaxWidths(headers []string, rows [][]string, maxWidths []int) {
	printBoxTableWithOptions(headers, rows, maxWidths, tableOptions{})
}

func printBoxTableWithOptions(headers []string, rows [][]string, maxWidths []int, options tableOptions) {
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

	fitWidthsToTerminal(widths, headers, options)

	fmt.Fprintln(os.Stdout, borderLine("┌", "┬", "┐", widths))
	fmt.Fprintln(os.Stdout, renderRow(headers, widths))
	fmt.Fprintln(os.Stdout, borderLine("├", "┼", "┤", widths))
	for rowIdx, row := range rows {
		for _, rendered := range renderWrappedRows(rowIdx, row, widths, options) {
			fmt.Fprintln(os.Stdout, rendered)
		}
		if options.rowDividers && rowIdx < len(rows)-1 {
			fmt.Fprintln(os.Stdout, borderLine("├", "┼", "┤", widths))
		}
	}
	fmt.Fprintln(os.Stdout, borderLine("└", "┴", "┘", widths))
}

func fitWidthsToTerminal(widths []int, headers []string, options tableOptions) {
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
		if i < len(options.minWidths) && options.minWidths[i] > minWidth {
			minWidth = options.minWidths[i]
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

func renderWrappedRows(rowIdx int, row []string, widths []int, options tableOptions) []string {
	cellLines := make([][]string, len(widths))
	maxLines := 1
	for i, width := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		lines := wrapCell(cell, width, noWrapCell(options, rowIdx, i))
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

func noWrapCell(options tableOptions, row int, col int) bool {
	if len(options.noWrapCells) == 0 {
		return false
	}
	_, ok := options.noWrapCells[cellKey(row, col)]
	return ok
}

func wrapCell(value string, width int, noWrap bool) []string {
	if width <= 0 {
		return []string{value}
	}
	if value == "" {
		return []string{""}
	}
	if noWrap {
		paragraphs := strings.Split(value, "\n")
		lines := make([]string, 0, len(paragraphs))
		for _, paragraph := range paragraphs {
			lines = append(lines, truncateCell(strings.TrimRight(paragraph, " "), width))
		}
		return lines
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

func firstNonEmptyOutput(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
	for _, fd := range []uintptr{os.Stdout.Fd(), os.Stderr.Fd(), os.Stdin.Fd()} {
		if width, _, err := term.GetSize(int(fd)); err == nil && width >= 40 {
			return width
		}
	}
	if value := strings.TrimSpace(os.Getenv("COLUMNS")); value != "" {
		if width, err := strconv.Atoi(value); err == nil && width >= 40 && width <= 400 {
			return width
		}
	}
	return 100
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
