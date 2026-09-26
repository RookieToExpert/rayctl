package output

import (
	"fmt"
	"os"
	"rayctl/internal/service"
	"strconv"
)

func PrintVCList(result *service.VCListResult) {
	if captureJSON(result) {
		return
	}
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
	if captureJSON(result) {
		return
	}
	PrintVCOverview(result, nil)
}

func PrintVCOverview(result *service.VCDetailResult, mapping *service.ClusterGetResult) {
	if captureJSON(map[string]any{"resource": result, "mapping": mapping}) {
		return
	}
	printVCOverviewTable(result, mapping)
	if mapping != nil {
		printClusterNamespaceMappings(mapping.ResourceNamespaces)
	}
}

func PrintVCOverviewSummary(result *service.VCDetailResult, mapping *service.ClusterGetResult) {
	if captureJSON(map[string]any{"resource": result, "mapping": mapping}) {
		return
	}
	printVCOverviewTable(result, mapping)
}

func printVCOverviewTable(result *service.VCDetailResult, mapping *service.ClusterGetResult) {
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
	if mapping != nil {
		rows = append(rows,
			[]string{"CONTROL PLANE NS", emptyDash(mapping.ControlPlaneNamespace)},
			[]string{"RESOURCE NS COUNT", fmt.Sprintf("%d", mapping.ResourceNamespaceCount)},
		)
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{20, 72},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 1)...),
			minWidths:   []int{12, 24},
		},
	)
}

func PrintVCNodeList(result *service.VCNodeListResult, longOutput bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"VC", emptyDash(result.ClusterName)},
			{"VC UID", emptyDash(result.ClusterUID)},
			{"PROFILE", emptyDash(result.ProfileName)},
			{"NODE COUNT", strconv.Itoa(len(result.Items))},
		},
		[]int{14, 72},
		tableOptions{minWidths: []int{10, 24}},
	)
	fmt.Fprintln(os.Stdout)
	printVCNodeListItems(result.Items, longOutput)
}

func PrintVCNodeListMany(results []*service.VCNodeListResult, longOutput bool) {
	if captureJSON(results) {
		return
	}
	rows := make([][]string, 0)
	for _, result := range results {
		for _, item := range result.Items {
			if longOutput {
				rows = append(rows, []string{emptyDash(result.ClusterName), emptyDash(item.HostName), emptyDash(item.Model), emptyDash(item.Name), emptyDash(item.UID)})
			} else {
				rows = append(rows, []string{emptyDash(result.ClusterName), emptyDash(item.HostName), emptyDash(item.HostIP), emptyDash(item.State), emptyDash(item.MachineType)})
			}
		}
	}
	headers := []string{"VC", "HOST", "IP", "STATE", "MACHINE TYPE"}
	widths := []int{28, 20, 15, 10, 18}
	minimums := []int{14, 16, 15, 8, 12}
	if longOutput {
		headers = []string{"VC", "HOST", "MODEL", "ACN", "ACN UID"}
		widths = []int{28, 22, 24, 48, 36}
		minimums = []int{16, 20, 14, 28, 36}
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for index := range row {
			row[index] = "-"
		}
		rows = append(rows, row)
	}
	noWrapColumns := []int{len(headers) - 1}
	if longOutput {
		noWrapColumns = []int{1, 3, 4}
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{
		noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), noWrapColumns...)...),
		minWidths:   minimums,
	})
}

func printVCNodeListItems(items []service.VCNodeListItem, longOutput bool) {
	headers := []string{"HOST", "IP", "STATE", "MACHINE TYPE"}
	widths := []int{22, 16, 10, 20}
	minimums := []int{20, 15, 8, 12}
	if longOutput {
		headers = []string{"HOST", "MODEL", "ACN", "ACN UID"}
		widths = []int{22, 24, 52, 36}
		minimums = []int{20, 14, 32, 36}
	}
	rows := make([][]string, 0, maxInt(1, len(items)))
	for _, item := range items {
		if longOutput {
			rows = append(rows, []string{emptyDash(item.HostName), emptyDash(item.Model), emptyDash(item.Name), emptyDash(item.UID)})
		} else {
			rows = append(rows, []string{emptyDash(item.HostName), emptyDash(item.HostIP), emptyDash(item.State), emptyDash(item.MachineType)})
		}
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for index := range row {
			row[index] = "-"
		}
		rows = append(rows, row)
	}
	noWrapColumns := []int{len(headers) - 1}
	if longOutput {
		noWrapColumns = []int{0, 2, 3}
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{
		noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), noWrapColumns...)...),
		minWidths:   minimums,
	})
}

func PrintVCNodeRemoveResult(result *service.VCNodeRemoveResult, longOutput bool, showPayload bool) {
	if result == nil {
		return
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"VC", emptyDash(result.ClusterName)},
			{"VC UID", emptyDash(result.ClusterUID)},
			{"PROFILE", emptyDash(result.ProfileName)},
			{"NODE COUNT", strconv.Itoa(len(result.Nodes))},
			{"RESULT", emptyDash(result.Result)},
		},
		[]int{14, 72},
		tableOptions{minWidths: []int{10, 24}},
	)
	fmt.Fprintln(os.Stdout)
	printVCNodeItems(result.Nodes, longOutput, true)
	if !showPayload {
		return
	}
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions(
		[]string{"REQUEST URL"},
		[][]string{{emptyDash(result.RequestURL)}},
		[]int{140},
		tableOptions{minWidths: []int{40}},
	)
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions(
		[]string{"PAYLOAD"},
		[][]string{{emptyDash(result.Payload)}},
		[]int{120},
		tableOptions{minWidths: []int{40}},
	)
}

func PrintVCNodeAddResult(result *service.VCNodeAddResult, longOutput bool, showPayload bool) {
	if result == nil {
		return
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"VC", emptyDash(result.ClusterName)},
			{"VC UID", emptyDash(result.ClusterUID)},
			{"PROFILE", emptyDash(result.ProfileName)},
			{"NODE COUNT", strconv.Itoa(len(result.Nodes))},
			{"RESULT", emptyDash(result.Result)},
		},
		[]int{14, 72},
		tableOptions{minWidths: []int{10, 24}},
	)
	fmt.Fprintln(os.Stdout)
	printVCNodeItems(result.Nodes, longOutput, true)
	if !showPayload {
		return
	}
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions([]string{"REQUEST URL"}, [][]string{{emptyDash(result.RequestURL)}}, []int{140}, tableOptions{minWidths: []int{40}})
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions([]string{"PAYLOAD"}, [][]string{{emptyDash(result.Payload)}}, []int{120}, tableOptions{minWidths: []int{40}})
}

func printVCNodeItems(items []service.VCNodeListItem, longOutput bool, includeACNName bool) {
	rows := make([][]string, 0, maxInt(1, len(items)))
	noWrapCells := make([][2]int, 0, maxInt(1, len(items))*4)
	headers := []string{"HOST", "IP", "STATE", "MACHINE TYPE", "MODEL"}
	maxWidths := []int{20, 15, 8, 14, 18}
	minWidths := []int{20, 15, 8, 12, 10}
	if longOutput {
		if includeACNName {
			headers = append(headers, "ACN")
			maxWidths = append(maxWidths, 14)
			minWidths = append(minWidths, 14)
		}
		headers = append(headers, "ACN UID")
		maxWidths = append(maxWidths, 36)
		minWidths = append(minWidths, 36)
	}
	if len(items) == 0 {
		row := []string{"-", "-", "-", "-", "-"}
		if longOutput {
			if includeACNName {
				row = append(row, "-")
			}
			row = append(row, "-")
		}
		rows = append(rows, row)
	} else {
		for rowIndex, item := range items {
			row := []string{
				emptyDash(item.HostName),
				emptyDash(item.HostIP),
				emptyDash(item.State),
				emptyDash(item.MachineType),
				emptyDash(item.Model),
			}
			if longOutput {
				if includeACNName {
					row = append(row, emptyDash(item.Name))
				}
				row = append(row, emptyDash(item.UID))
			}
			rows = append(rows, row)
			noWrapColumns := []int{0, 1}
			if longOutput {
				for columnIndex := 5; columnIndex < len(headers); columnIndex++ {
					noWrapColumns = append(noWrapColumns, columnIndex)
				}
			}
			for _, columnIndex := range noWrapColumns {
				noWrapCells = append(noWrapCells, [2]int{rowIndex, columnIndex})
			}
		}
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

func PrintClusterDetail(result *service.ClusterGetResult) {
	if captureJSON(result) {
		return
	}
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

	printClusterNamespaceMappings(result.ResourceNamespaces)
}

func printClusterNamespaceMappings(items []service.ClusterNamespaceMapping) {
	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, maxInt(1, len(items)))
	if len(items) == 0 {
		rows = append(rows, []string{"-", "-"})
	} else {
		for _, item := range items {
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
