package output

import "rayctl/internal/service"

func PrintSSPWorkspaceWorkloads(results []*service.SSPWorkspaceWorkloadResult) {
	if captureJSON(results) {
		return
	}
	rows := make([][]string, 0)
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			rows = append(rows, append([]string{emptyDash(item.Queue)}, workloadSummaryRow(item, result.Workspace.Name)...))
		}
		if len(result.Items) == 0 {
			rows = append(rows, append([]string{"-"}, workloadSummaryRow(service.SSPQueueWorkloadItem{}, result.Workspace.Name)...))
		}
	}
	printScopedWorkloadTable(rows, false)
}

func PrintVCWorkloads(results []*service.VCWorkloadResult) {
	if captureJSON(results) {
		return
	}
	scopes := 0
	for _, result := range results {
		if result != nil {
			scopes++
		}
	}
	multipleVCs := scopes > 1
	rows := make([][]string, 0)
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			row := append([]string{emptyDash(item.Queue)}, workloadSummaryRow(item, "")...)
			if multipleVCs {
				row = append([]string{emptyDash(result.ClusterName)}, row...)
			}
			rows = append(rows, row)
		}
		if len(result.Items) == 0 {
			row := append([]string{"-"}, workloadSummaryRow(service.SSPQueueWorkloadItem{}, "")...)
			if multipleVCs {
				row = append([]string{emptyDash(result.ClusterName)}, row...)
			}
			rows = append(rows, row)
		}
	}
	printScopedWorkloadTable(rows, multipleVCs)
}

func workloadSummaryRow(item service.SSPQueueWorkloadItem, workspace string) []string {
	if item.Workspace != "" {
		workspace = item.Workspace
	}
	return []string{
		formatScopedWorkloadType(item.Type), emptyDash(item.Name), emptyDash(item.State),
		emptyDash(workspace), emptyDash(item.Priority), emptyDash(item.Creator), emptyDash(item.CreatedAt),
	}
}

func formatScopedWorkloadType(value string) string {
	if value == "vcJob" || value == "vc-job" {
		return "VCJOB"
	}
	return formatSSPWorkloadType(value)
}

func printScopedWorkloadTable(rows [][]string, multipleVCs bool) {
	headers := []string{"QUEUE", "TYPE", "NAME", "STATE", "WORKSPACE", "PRIORITY", "CREATOR", "CREATED"}
	widths := []int{44, 10, 56, 14, 38, 12, 24, 20}
	minimums := []int{22, 6, 32, 10, 20, 10, 14, 19}
	noWrapColumns := []int{0, 2, 4, 6}
	if multipleVCs {
		headers = append([]string{"VC"}, headers...)
		widths = append([]int{34}, widths...)
		minimums = append([]int{18}, minimums...)
		noWrapColumns = []int{0, 1, 3, 5, 7}
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for index := range row {
			row[index] = "-"
		}
		rows = append(rows, row)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{
		minWidths:   minimums,
		noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), noWrapColumns...)...),
	})
}
