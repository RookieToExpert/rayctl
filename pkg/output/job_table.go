package output

import (
	"fmt"
	"os"
	"rayctl/internal/service"
	"strconv"
	"strings"
)

func PrintJobDetail(result *service.JobGetResult, longOutput bool, debugTiming bool) {
	if captureJSON(result) {
		return
	}
	if longOutput && result != nil {
		printATEIncrement(result, debugTiming)
		return
	}
	if longOutput {
		defer func() { printPodEvidence(result.PodEvidence) }()
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
	failedShown := 0
	for _, pod := range result.Pods {
		if strings.EqualFold(pod.Phase, "Failed") && failedShown < 3 {
			summaryRows = append(summaryRows, []string{"FAILED POD", fmt.Sprintf("%s: %s | %s", pod.Name, emptyDash(pod.Reason), emptyDash(pod.Message))})
			failedShown++
		}
	}
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
	if longOutput && result.PodEvidence == nil && shouldShowJobLogs(result) {
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
	if longOutput && result != nil {
		printAITIncrement(result)
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
	if longOutput && result != nil {
		printAIDIncrement(result, debugTiming)
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
		{"ENVIRONMENT", emptyDash(result.Environment)},
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
