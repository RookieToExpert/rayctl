package output

import (
	"fmt"
	"os"
	"rayctl/internal/service"
)

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
