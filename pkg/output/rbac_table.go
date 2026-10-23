package output

import (
	"fmt"
	"os"
	"rayctl/internal/service"
	"strconv"
	"strings"
)

func PrintRBACGetResult(result *service.RBACGetResult, long bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	if long {
		printRBACIncrement(result)
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
	if long {
		rows := make([][]string, 0, maxInt(1, len(result.Items)))
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.Kind),
				emptyDash(item.Namespace),
				emptyDash(item.Name),
				emptyDash(item.Role),
				emptyDash(item.Subjects),
				emptyDash(item.CreatedAt),
			})
		}
		if len(rows) == 0 {
			rows = append(rows, []string{"-", "-", "-", "-", "-", "-"})
		}
		noWrapCells := make([][2]int, 0, len(rows)*6)
		for column := 0; column < 6; column++ {
			noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), column)...)
		}
		printBoxTableWithOptions(
			[]string{"TYPE", "NAMESPACE", "BINDING", "ROLE", "SUBJECTS", "CREATE TIME"},
			rows,
			[]int{4, 20, 20, 13, 20, 19},
			tableOptions{
				noWrapCells: makeNoWrapCells(noWrapCells...),
				minWidths:   []int{4, 12, 12, 13, 10, 19},
				rowDividers: true,
			},
		)
		return
	}

	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Kind),
			emptyDash(item.Namespace),
			emptyDash(item.Role),
			emptyDash(item.Subjects),
			rbacCreateDate(item.CreatedAt),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-"})
	}
	noWrapCells := make([][2]int, 0, len(rows)*5)
	for column := 0; column < 5; column++ {
		noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), column)...)
	}
	printBoxTableWithOptions(
		[]string{"TYPE", "NAMESPACE", "ROLE", "SUBJECTS", "DATE"},
		rows,
		[]int{4, 24, 13, 20, 10},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{4, 12, 13, 10, 10},
			rowDividers: true,
		},
	)
}

func PrintRBACGetSummary(results []*service.RBACGetResult, includeVC bool, long bool) {
	if captureJSON(results) {
		return
	}
	if long {
		for _, result := range results {
			if result != nil {
				printRBACIncrement(result)
			}
		}
		return
	}
	rows := make([][]string, 0)
	for _, result := range results {
		for _, item := range result.Items {
			row := []string{emptyDash(item.Namespace), emptyDash(item.Role), emptyDash(item.Subjects)}
			if includeVC {
				row = append([]string{emptyDash(result.ClusterName)}, row...)
			}
			if long {
				row = append(row, emptyDash(item.Kind), emptyDash(item.Name), emptyDash(item.CreatedAt))
			}
			rows = append(rows, row)
		}
	}
	headers := []string{"NAMESPACE", "ROLE", "SUBJECTS"}
	widths := []int{28, 16, 32}
	minimums := []int{12, 10, 14}
	if includeVC {
		headers = append([]string{"VC"}, headers...)
		widths = append([]int{28}, widths...)
		minimums = append([]int{14}, minimums...)
	}
	if long {
		headers = append(headers, "TYPE", "BINDING", "CREATE TIME")
		widths = append(widths, 5, 32, 19)
		minimums = append(minimums, 4, 14, 19)
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for i := range row {
			row[i] = "-"
		}
		rows = append(rows, row)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{minWidths: minimums, rowDividers: true})
}

func rbacCreateDate(createdAt string) string {
	fields := strings.Fields(strings.TrimSpace(createdAt))
	if len(fields) == 0 {
		return "-"
	}
	return fields[0]
}

func PrintRBACGrantResult(result *service.RBACGrantResult) {
	if result == nil {
		return
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"VC", emptyDash(result.ClusterName)},
			{"VC UID", emptyDash(result.ClusterUID)},
			{"PROFILE", emptyDash(result.ProfileName)},
			{"NAMESPACE", emptyDash(result.Namespace)},
			{"BINDING KIND", emptyDash(result.BindingKind)},
			{"ROLE", emptyDash(result.Role)},
			{"ACCESS", emptyDash(result.AccessReason)},
			{"RESULT", emptyDash(result.Result)},
		},
		[]int{16, 104},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(8, 1)...),
			minWidths:   []int{12, 28},
		},
	)

	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, maxInt(1, len(result.Members)))
	if len(result.Members) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-"})
	} else {
		for _, member := range result.Members {
			rows = append(rows, []string{
				emptyDash(member.Type),
				emptyDash(member.Name),
				emptyDash(member.DisplayName),
				emptyDash(member.ID),
				emptyDash(member.Status),
			})
		}
	}
	noWrapCells := make([][2]int, 0, len(rows)*4)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 0)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 1)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 3)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 4)...)
	printBoxTableWithOptions(
		[]string{"TYPE", "NAME", "DISPLAY NAME", "ID", "STATUS"},
		rows,
		[]int{8, 28, 28, 36, 18},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{6, 12, 12, 32, 12},
		},
	)

	if result.Result != "dry-run" || strings.TrimSpace(result.Payload) == "" {
		return
	}
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions(
		[]string{"PAYLOAD"},
		[][]string{{result.Payload}},
		[]int{120},
		tableOptions{minWidths: []int{40}},
	)
}

func PrintRBACRemoveResult(result *service.RBACRemoveResult) {
	if result == nil {
		return
	}
	rows := [][]string{
		{"VC", emptyDash(result.ClusterName)},
		{"VC UID", emptyDash(result.ClusterUID)},
		{"NAMESPACE", emptyDash(result.Namespace)},
		{"BINDING KIND", emptyDash(result.BindingKind)},
		{"BINDING", emptyDash(result.BindingName)},
		{"ROLE", emptyDash(result.Role)},
		{"AFFECTED", strconv.Itoa(result.SubjectCount)},
		{"SUBJECTS", emptyDash(result.Subjects)},
		{"ACCESS", emptyDash(result.AccessReason)},
		{"RESULT", emptyDash(result.Result)},
	}
	noWrapCells := make([][2]int, 0, len(rows)-1)
	for row := range rows {
		if row != 7 {
			noWrapCells = append(noWrapCells, [2]int{row, 1})
		}
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{16, 80},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{12, 24},
		},
	)
}
