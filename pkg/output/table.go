package output

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"rayctl/internal/service"

	"golang.org/x/term"
)

type tableOptions struct {
	noWrapCells map[string]struct{}
	minWidths   []int
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

func PrintNodeList(nodes []service.NodeListItem, resolvedSelector string, total int, start int, end int, limit int, showProdRole bool, longOutput bool) {
	if resolvedSelector == "" {
		fmt.Fprintln(os.Stdout, "Selector: <all nodes>")
	} else {
		fmt.Fprintf(os.Stdout, "Selector: %s\n", truncateForWidth(resolvedSelector, terminalWidth()-10))
	}

	rows := make([][]string, 0, len(nodes))
	noWrapCells := make([][2]int, 0, len(nodes)*8)
	for rowIdx, node := range nodes {
		row := []string{
			node.Name,
			yesNoFromReady(node.Ready),
			yesNo(node.Schedulable == "Yes"),
		}
		if showProdRole {
			row = append(row, emptyDash(node.ProdRole))
		}
		row = append(row,
			yesNo(node.Repair),
			node.InternalIP,
			node.ClusterName,
		)
		if longOutput {
			row = append(row, emptyDash(node.Tenant))
		}
		rows = append(rows, row)
		for colIdx := range row {
			noWrapCells = append(noWrapCells, [2]int{rowIdx, colIdx})
		}
	}

	headers := []string{"HOST", "RDY", "SCH"}
	maxWidths := []int{22, 5, 5}
	minWidths := []int{19, 5, 5}
	if showProdRole {
		headers = append(headers, "PROD")
		maxWidths = append(maxWidths, 18)
		minWidths = append(minWidths, 11)
	}
	headers = append(headers, "RPR", "IP", "CLUSTERNAME")
	maxWidths = append(maxWidths, 3, 16, 48)
	minWidths = append(minWidths, 3, 14, 16)
	if longOutput {
		if showProdRole {
			maxWidths[0] = 19
			minWidths[0] = 19
			maxWidths[3] = 11
			minWidths[3] = 11
			maxWidths[5] = 14
			minWidths[5] = 14
			maxWidths[6] = 18
			minWidths[6] = 12
		} else {
			maxWidths[0] = 19
			minWidths[0] = 19
			maxWidths[4] = 14
			minWidths[4] = 14
			maxWidths[5] = 18
			minWidths[5] = 12
		}
		headers = append(headers, "TENANT")
		maxWidths = append(maxWidths, 12)
		minWidths = append(minWidths, 8)
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

func PrintNodeDescribe(details *service.NodeDescribe, debugTiming bool, clientDuration interface{}) {
	printBoxTableWithOptions(
		[]string{"HOST", "VC", "UNSCH", "RPR", "GPU", "CPU", "MEM", "PODS"},
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
		[]int{24, 20, 5, 5, 8, 12, 12, 5},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 0},
				[2]int{0, 1},
			),
		},
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
		{"STATUS", emptyDash(result.Status)},
		{"VC", result.VClusterName},
		{"NAMESPACE", result.Namespace},
		{"UID", result.UID},
		{"SUBMITTER", result.Submitter},
		{"PODGROUP", result.PodGroupName},
		{"IMAGE PULL SECRET", joinOrDash(result.ImagePullSecrets)},
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
				emptyDash(pvc.FrontendVolume),
			})
		}
		printBoxTableWithMaxWidths(
			[]string{"VIRTUAL PVC", "AFS/AOSS ENDPOINT"},
			pvcRows,
			[]int{32, 32},
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
			if shouldShowJobLogs(result) {
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
		}
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

func PrintJobClusterList(result *service.JobClusterListResult) {
	summaryRows := [][]string{
		{"分区名", emptyDash(result.ClusterName)},
		{"活跃任务数量", fmt.Sprintf("%d", result.ActiveJobCount)},
		{"活跃 Pod 数量", fmt.Sprintf("%d", result.ActivePodCount)},
		{"总任务数量", fmt.Sprintf("%d", result.TotalJobCount)},
		{"总 Pod 数量", fmt.Sprintf("%d", result.TotalPodCount)},
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

func PrintUserDetail(result *service.UserGetResult) {
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
			),
			minWidths: []int{10, 20},
		},
	)

	if len(result.Jobs) == 0 {
		return
	}

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

func PrintAuthAFSResult(result *service.AuthAFSResult) {
	if result == nil {
		return
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"AFS", emptyDash(result.AFSName)},
			{"授权数量", strconv.Itoa(len(result.Items))},
		},
		[]int{14, 64},
		tableOptions{
			noWrapCells: makeNoWrapCells([2]int{0, 1}, [2]int{1, 1}),
			minWidths:   []int{10, 20},
		},
	)

	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	if len(result.Items) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-"})
	} else {
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.MemberType),
				emptyDash(item.MemberName),
				emptyDash(item.MemberIdentify),
				emptyDash(item.MemberValue),
				emptyDash(item.Roles),
				emptyDash(item.RoleNames),
				emptyDash(item.CreateTime),
			})
		}
	}

	noWrapCells := make([][2]int, 0, len(rows)*2)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 0)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 6)...)
	printBoxTableWithOptions(
		[]string{"TYPE", "NAME", "IDENTIFY", "ID", "ROLES", "ROLE NAMES", "CREATE TIME"},
		rows,
		[]int{8, 28, 28, 36, 36, 36, 19},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{6, 10, 10, 18, 12, 12, 19},
		},
	)
}

func PrintAuthUserResult(result *service.AuthUserResult, long bool) {
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
			{"用户组数量", strconv.Itoa(len(result.Groups))},
			{"权限数量", strconv.Itoa(len(result.Permissions))},
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
				[2]int{7, 1},
			),
			minWidths: []int{10, 20},
		},
	)

	fmt.Fprintln(os.Stdout)
	permissionRows := make([][]string, 0, maxInt(1, len(result.Permissions)))
	if !long {
		if len(result.Permissions) == 0 {
			permissionRows = append(permissionRows, []string{"-", "-"})
		} else {
			for _, item := range result.Permissions {
				permissionRows = append(permissionRows, []string{
					emptyDash(formatAuthScopeForDisplay(item.Scope)),
					emptyDash(item.Roles),
				})
			}
		}
		printBoxTableWithOptions(
			[]string{"SCOPE", "ROLES"},
			permissionRows,
			[]int{56, 36},
			tableOptions{
				minWidths: []int{18, 10},
			},
		)
		return
	}

	groupRows := make([][]string, 0, maxInt(1, len(result.Groups)))
	if len(result.Groups) == 0 {
		groupRows = append(groupRows, []string{"-", "-", "-", "-"})
	} else {
		for _, group := range result.Groups {
			groupRows = append(groupRows, []string{
				emptyDash(firstNonEmptyOutput(group.DisplayName, group.Name, group.PosixGroupName)),
				emptyDash(group.PosixGroupName),
				emptyDash(group.ID),
				emptyDash(group.Status),
			})
		}
	}
	groupNoWrap := make([][2]int, 0, len(groupRows))
	groupNoWrap = append(groupNoWrap, noWrapCellsForSingleColumn(len(groupRows), 3)...)
	printBoxTableWithOptions(
		[]string{"GROUP", "POSIX", "ID", "STATUS"},
		groupRows,
		[]int{32, 28, 36, 10},
		tableOptions{
			noWrapCells: makeNoWrapCells(groupNoWrap...),
			minWidths:   []int{10, 10, 18, 8},
		},
	)

	fmt.Fprintln(os.Stdout)
	permissionRows = make([][]string, 0, maxInt(1, len(result.Permissions)))
	if len(result.Permissions) == 0 {
		permissionRows = append(permissionRows, []string{"-", "-", "-", "-", "-", "-", "-"})
	} else {
		for _, item := range result.Permissions {
			permissionRows = append(permissionRows, []string{
				emptyDash(item.Source),
				emptyDash(item.Member),
				emptyDash(item.Service),
				emptyDash(formatAuthScopeForDisplay(item.Scope)),
				emptyDash(item.Roles),
				emptyDash(item.RoleNames),
				emptyDash(item.CreateTime),
			})
		}
	}
	permissionNoWrap := make([][2]int, 0, len(permissionRows)*3)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 0)...)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 2)...)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 6)...)
	printBoxTableWithOptions(
		[]string{"SOURCE", "MEMBER", "SERVICE", "SCOPE", "ROLES", "ROLE NAMES", "CREATE TIME"},
		permissionRows,
		[]int{8, 24, 10, 56, 28, 28, 19},
		tableOptions{
			noWrapCells: makeNoWrapCells(permissionNoWrap...),
			minWidths:   []int{6, 10, 8, 18, 10, 12, 19},
		},
	)
}

func formatAuthScopeForDisplay(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" || scope == "-" {
		return scope
	}
	scope = strings.TrimPrefix(scope, "/rm")
	scope = strings.Trim(scope, "/")
	if scope == "" {
		return "租户级"
	}
	parts := strings.Split(scope, "/")
	if len(parts) == 1 {
		switch strings.ToLower(parts[0]) {
		case "tenant", "tenants":
			return "租户级"
		}
	}
	if len(parts) >= 2 {
		switch parts[0] {
		case "managementGroups", "managementgroups":
			if len(parts) == 2 {
				return "管理组"
			}
			return parts[len(parts)-1]
		case "subscriptions":
			if len(parts) == 2 {
				return "订阅级"
			}
			if len(parts) == 4 && parts[2] == "resourceGroups" {
				return "资源组 " + parts[3]
			}
			if len(parts) == 6 && parts[2] == "resourceGroups" && parts[4] == "zones" {
				return "可用区 " + parts[5]
			}
			if len(parts) == 6 && parts[2] == "resourceGroups" && parts[4] == "regions" {
				return "地域 " + parts[5]
			}
			if len(parts) > 4 {
				return parts[len(parts)-1]
			}
		case "tenants", "tenant":
			return "租户级"
		}
	}
	return scope
}

func PrintAuthGroupResult(result *service.AuthGroupResult) {
	if result == nil {
		return
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"ID", emptyDash(result.ID)},
			{"NAME", emptyDash(result.Name)},
			{"DISPLAY NAME", emptyDash(result.DisplayName)},
			{"POSIX", emptyDash(result.PosixGroupName)},
			{"TENANT CODE", emptyDash(result.TenantCode)},
			{"STATUS", emptyDash(result.Status)},
			{"权限数量", strconv.Itoa(len(result.Permissions))},
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

	fmt.Fprintln(os.Stdout)
	permissionRows := make([][]string, 0, maxInt(1, len(result.Permissions)))
	if len(result.Permissions) == 0 {
		permissionRows = append(permissionRows, []string{"-", "-", "-", "-", "-", "-"})
	} else {
		for _, item := range result.Permissions {
			permissionRows = append(permissionRows, []string{
				emptyDash(item.Member),
				emptyDash(item.Service),
				emptyDash(item.Scope),
				emptyDash(item.Roles),
				emptyDash(item.RoleNames),
				emptyDash(item.CreateTime),
			})
		}
	}
	permissionNoWrap := make([][2]int, 0, len(permissionRows)*2)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 1)...)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 5)...)
	printBoxTableWithOptions(
		[]string{"GROUP", "SERVICE", "SCOPE", "ROLES", "ROLE NAMES", "CREATE TIME"},
		permissionRows,
		[]int{24, 10, 64, 28, 28, 19},
		tableOptions{
			noWrapCells: makeNoWrapCells(permissionNoWrap...),
			minWidths:   []int{10, 8, 18, 10, 12, 19},
		},
	)
}

func PrintAuthGrantAFSResult(result *service.AuthGrantAFSResult) {
	if result == nil {
		return
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"AFS", emptyDash(result.AFSName)},
			{"SCOPE", emptyDash(result.Scope)},
			{"MEMBER TYPE", emptyDash(result.MemberType)},
			{"MEMBER NAME", emptyDash(result.MemberName)},
			{"MEMBER IDENTIFY", emptyDash(result.MemberIdentify)},
			{"MEMBER ID", emptyDash(result.MemberValue)},
			{"ROLE", emptyDash(result.RoleName)},
			{"ROLE ID", emptyDash(result.RoleID)},
			{"RESULT", emptyDash(result.Result)},
			{"POLICY ID", emptyDash(result.PolicyID)},
		},
		[]int{16, 104},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{2, 1},
				[2]int{4, 1},
				[2]int{5, 1},
				[2]int{6, 1},
				[2]int{7, 1},
				[2]int{8, 1},
				[2]int{9, 1},
			),
			minWidths: []int{12, 28},
		},
	)

	if strings.TrimSpace(result.Payload) == "" {
		return
	}
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions(
		[]string{"PAYLOAD"},
		[][]string{{result.Payload}},
		[]int{120},
		tableOptions{
			minWidths: []int{40},
		},
	)
}

func PrintRBACGetResult(result *service.RBACGetResult) {
	if result == nil {
		return
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"VC", emptyDash(result.ClusterName)},
			{"VC UID", emptyDash(result.ClusterUID)},
			{"CLUSTER REF", emptyDash(result.ClusterRef)},
			{"PROFILE", emptyDash(result.ProfileName)},
			{"SELECTOR", emptyDash(result.LabelSelector)},
			{"绑定数量", strconv.Itoa(len(result.Items))},
		},
		[]int{14, 96},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
				[2]int{4, 1},
				[2]int{5, 1},
			),
			minWidths: []int{10, 24},
		},
	)

	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	if len(result.Items) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-"})
	} else {
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.Name),
				emptyDash(item.Role),
				emptyDash(item.Subjects),
				emptyDash(item.CreatedAt),
			})
		}
	}
	noWrapCells := make([][2]int, 0, len(rows)*2)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 1)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 3)...)
	printBoxTableWithOptions(
		[]string{"BINDING", "ROLE", "SUBJECTS", "CREATE TIME"},
		rows,
		[]int{48, 36, 56, 19},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{16, 14, 18, 19},
		},
	)
}

func PrintVCList(result *service.VCListResult) {
	if result == nil {
		return
	}

	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	if len(result.Items) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-"})
	} else {
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.Name),
				emptyDash(item.UID),
				emptyDash(item.Tenant),
				emptyDash(item.Region),
				emptyDash(item.State),
			})
		}
	}

	printBoxTableWithOptions(
		[]string{"VC", "UID", "TENANT", "REGION", "STATE"},
		rows,
		[]int{32, 36, 18, 12, 12},
		tableOptions{
			noWrapCells: makeNoWrapCells(append(
				noWrapCellsForSingleColumn(len(rows), 1),
				noWrapCellsForSingleColumn(len(rows), 3)...,
			)...),
			minWidths: []int{18, 36, 8, 8, 8},
		},
	)
}

func PrintVCDetail(result *service.VCDetailResult) {
	if result == nil {
		return
	}

	rows := [][]string{
		{"VC", emptyDash(result.Name)},
		{"UID", emptyDash(result.UID)},
		{"TENANT", emptyDash(result.Tenant)},
		{"REGION", emptyDash(result.Region)},
		{"STATE", emptyDash(result.State)},
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{12, 72},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 1)...),
			minWidths:   []int{8, 24},
		},
	)
}

func PrintClusterDetail(result *service.ClusterGetResult) {
	if result == nil {
		return
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"分区名", emptyDash(result.ClusterName)},
			{"VC UID", emptyDash(result.ClusterUID)},
			{"控制面 namespace", emptyDash(result.ControlPlaneNamespace)},
			{"资源 namespace 数量", fmt.Sprintf("%d", result.ResourceNamespaceCount)},
		},
		[]int{20, 84},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
			),
			minWidths: []int{16, 32},
		},
	)

	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, maxInt(1, len(result.ResourceNamespaces)))
	if len(result.ResourceNamespaces) == 0 {
		rows = append(rows, []string{"-", "-"})
	} else {
		for _, item := range result.ResourceNamespaces {
			rows = append(rows, []string{
				emptyDash(item.ResourceNamespace),
				emptyDash(item.VirtualNamespace),
			})
		}
	}

	printBoxTableWithOptions(
		[]string{"RESOURCE NAMESPACE", "VIRTUAL NAMESPACE"},
		rows,
		[]int{36, 48},
		tableOptions{
			noWrapCells: makeNoWrapCells(append(
				noWrapCellsForSingleColumn(len(rows), 0),
				noWrapCellsForSingleColumn(len(rows), 1)...,
			)...),
			minWidths: []int{24, 24},
		},
	)
}

func PrintPolicyUpdateResult(result *service.PolicyUpdateResult) {
	if result == nil {
		return
	}

	status := "updated"
	if result.AlreadyPresent {
		status = "already exists"
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"POLICY", emptyDash(result.PolicyName)},
			{"VC", emptyDash(result.ClusterName)},
			{"VC UID", emptyDash(result.ClusterUID)},
			{"RULE", emptyDash(result.RuleName)},
			{"SELECTOR", emptyDash(result.SelectorKey + "=" + result.SelectorValue)},
			{"RESULT", status},
		},
		[]int{14, 96},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
				[2]int{4, 1},
				[2]int{5, 1},
			),
			minWidths: []int{10, 28},
		},
	)
}

func PrintPolicyGetResult(result *service.PolicyGetResult) {
	if result == nil {
		return
	}

	if strings.TrimSpace(result.TargetCluster) != "" || strings.TrimSpace(result.TargetUID) != "" {
		matchStatus := "no"
		if result.Matched {
			matchStatus = "yes"
		}
		selector := strings.TrimSpace(result.TargetSelector)
		if len(result.Items) > 0 {
			selectors := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				selectors = append(selectors, policySelectorText(item))
			}
			selector = strings.Join(selectors, "; ")
		}

		printBoxTableWithOptions(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"POLICY", emptyDash(result.PolicyName)},
				{"VC", emptyDash(result.TargetCluster)},
				{"VC UID", emptyDash(result.TargetUID)},
				{"MATCH", matchStatus},
				{"SELECTOR", emptyDash(selector)},
			},
			[]int{14, 112},
			tableOptions{
				noWrapCells: makeNoWrapCells(
					[2]int{0, 1},
					[2]int{1, 1},
					[2]int{2, 1},
					[2]int{3, 1},
					[2]int{4, 1},
				),
				minWidths: []int{10, 28},
			},
		)
		return
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"POLICY", emptyDash(result.PolicyName)},
			{"白名单 VC 数量", strconv.Itoa(len(result.Items))},
		},
		[]int{18, 64},
		tableOptions{
			noWrapCells: makeNoWrapCells([2]int{0, 1}, [2]int{1, 1}),
			minWidths:   []int{14, 24},
		},
	)

	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	if len(result.Items) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-"})
	} else {
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.ClusterName),
				emptyDash(item.ClusterUID),
				emptyDash(item.Tenant),
				emptyDash(policySelectorText(item)),
			})
		}
	}

	noWrapCells := make([][2]int, 0, len(rows)*4)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 0)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 1)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 2)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 3)...)
	printBoxTableWithOptions(
		[]string{"VC", "UID", "TENANT", "SELECTOR"},
		rows,
		[]int{32, 36, 18, 96},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{20, 32, 12, 28},
		},
	)
}

func policySelectorText(item service.PolicyWhitelistItem) string {
	selectorKey := strings.TrimSpace(item.SelectorKey)
	selectorValue := strings.TrimSpace(item.SelectorValue)
	if selectorKey == "" {
		return selectorValue
	}
	if selectorValue == "" {
		return selectorKey
	}
	return selectorKey + "=" + selectorValue
}

func PrintPVCheckDetail(result *service.PVCheckResult) {
	if result == nil {
		return
	}

	rows := [][]string{
		{"HOST PV", emptyDash(result.HostPVName)},
		{"HOST PVC", emptyDash(result.HostPVCName)},
		{"AFS", emptyDash(result.AFSName)},
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
	if result == nil || result.Terminal {
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
	rows := [][]string{
		{"AFS", emptyDash(result.AFSName)},
		{"HOST PVC", joinOrDash(result.HostPVCs)},
		{"HOST PV", joinOrDash(result.HostPVs)},
	}
	if longOutput {
		rows = append(rows, []string{"租户", emptyDash(result.Tenant)})
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{12, 120},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 1)...),
		},
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

func PrintPVCCheckDetail(result *service.PVCCheckResult, longOutput bool) {
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
		return []string{truncateCell(value, width)}
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
