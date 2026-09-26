package output

import (
	"fmt"
	"os"
	"rayctl/internal/service"
	"strconv"
	"strings"
)

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
