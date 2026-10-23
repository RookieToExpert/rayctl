package output

import (
	"fmt"
	"net"
	"os"
	"rayctl/internal/service"
)

func PrintECSList(result *service.ECSListResult, long bool) {
	if captureJSON(result) || result == nil {
		return
	}
	base := []string{"NAME", "TYPE", "STATE", "INTERNAL IP", "CREATOR"}
	headers := append([]string{}, base...)
	widths := []int{64, 6, 16, 40, 32}
	minimums := []int{20, 4, 10, 12, 12}
	noWrapColumns := []int{0, 3, 4}
	if long {
		headers = append(headers, "TENANT", "UID", "MACHINE", "VPC")
		widths = append(widths, 32, 36, 32, 64)
		minimums = append(minimums, 8, 36, 12, 16)
		noWrapColumns = append(noWrapColumns, 5, 6, 7, 8)
	}
	rows := make([][]string, 0, len(result.Items))
	for _, item := range result.Items {
		row := []string{emptyDash(item.Name), emptyDash(item.ResourceType), emptyDash(item.State), emptyDash(item.InternalIP), emptyDash(item.Creator)}
		if long {
			row = append(row, emptyDash(item.Tenant), emptyDash(item.UID), emptyDash(item.MachineType), emptyDash(item.VPC))
		}
		for _, column := range noWrapColumns {
			minimums[column] = max(minimums[column], min(widths[column], textWidth(row[column])))
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for index := range row {
			row[index] = "-"
		}
		rows = append(rows, row)
	}
	printListTable(base, headers, rows, widths, tableOptions{
		minWidths:   minimums,
		noWrapCells: makeNoWrapCells(noWrapCellsForColumns(len(rows), noWrapColumns...)...),
	}, long)
	fmt.Fprintf(os.Stdout, "Showing %d of %d matching resources", len(result.Items), result.Total)
	if result.Truncated {
		fmt.Fprint(os.Stdout, "; use -n 0 to show all")
	}
	fmt.Fprintln(os.Stdout)
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
				{"租户", emptyDash(item.Tenant)},
				{"租户 ID", emptyDash(item.TenantID)},
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
		printECSDNAT(item)
	}
}

func printECSDNAT(item service.ECSCheckItem) {
	fmt.Fprintln(os.Stdout)
	if len(item.DNATRules) > 0 {
		rows := make([][]string, 0, len(item.DNATRules))
		for _, rule := range item.DNATRules {
			rows = append(rows, []string{net.JoinHostPort(rule.ExternalIP, rule.ExternalPort), net.JoinHostPort(rule.InternalIP, rule.InternalPort), emptyDash(rule.Protocol), emptyDash(rule.State), emptyDash(rule.NATGateway)})
		}
		printBoxTableWithMaxWidths([]string{"EXTERNAL", "INTERNAL", "PROTOCOL", "STATE", "NAT GATEWAY"}, rows, []int{44, 44, 12, 16, 40})
	} else if item.DNATComplete {
		fmt.Fprintln(os.Stdout, "DNAT: 未发现端口映射。")
	}
	if !item.DNATComplete {
		fmt.Fprintln(os.Stdout, "DNAT: 查询未完成。")
	}
	for _, warning := range item.Warnings {
		fmt.Fprintln(os.Stdout, "Warning:", redactText(warning))
	}
}
