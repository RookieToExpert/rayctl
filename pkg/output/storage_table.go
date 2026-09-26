package output

import (
	"fmt"
	"os"
	"rayctl/internal/service"
	"strings"
)

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
