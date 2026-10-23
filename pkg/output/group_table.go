package output

import (
	"fmt"
	"os"
	"rayctl/internal/service"
	"strconv"
)

func PrintGroupGetResult(result *service.GroupGetResult, long bool) {
	if captureJSON(result) || result == nil {
		return
	}
	if long {
		printDetailAnchor(result.Name, result.Status, "", "", "")
		fields := [][]string{}
		if result.PosixGroupName != "" {
			fields = append(fields, []string{"POSIX", result.PosixGroupName})
		}
		if result.TenantID != "" {
			fields = append(fields, []string{"TENANT ID", result.TenantID})
		}
		if len(fields) > 0 {
			printResourceDetail(fields)
		}
		rows := [][]string{}
		for _, member := range result.Members {
			rows = append(rows, []string{member.Username, member.ID, yesNo(member.InGroup)})
		}
		if len(rows) > 0 {
			printBoxTable([]string{"USERNAME", "ID", "IN GROUP"}, rows)
		}
		return
	}
	fields := [][]string{
		{"ID", emptyDash(result.ID)},
		{"NAME", emptyDash(result.Name)},
		{"DISPLAY NAME", emptyDash(result.DisplayName)},
	}
	if long {
		if result.PosixGroupName != "" {
			fields = append(fields, []string{"POSIX", result.PosixGroupName})
		}
		if result.TenantID != "" {
			fields = append(fields, []string{"TENANT ID", result.TenantID})
		}
		if result.Status != "" {
			fields = append(fields, []string{"STATUS", result.Status})
		}
	}
	fields = append(fields,
		[]string{"成员数量", strconv.Itoa(result.MemberCount)},
		[]string{"协管员数量", strconv.Itoa(result.AdminCount)},
	)
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		fields,
		[]int{14, 72},
		tableOptions{minWidths: []int{10, 20}},
	)
	printAuthGroupMembers(result, long)
}

func PrintGroupList(items []service.GroupListItem, long bool) {
	if captureJSON(items) {
		return
	}
	headers := []string{"GROUP", "DISPLAY NAME"}
	base := append([]string{}, headers...)
	widths := []int{40, 40}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{emptyDash(item.Name), emptyDash(item.DisplayName)}
		if long {
			row = append(row, emptyDash(item.ID), emptyDash(item.PosixGroupName), emptyDash(item.TenantID), emptyDash(item.CreateTime))
		}
		rows = append(rows, row)
	}
	if long {
		headers = append(headers, "ID", "POSIX", "TENANT ID", "CREATED")
		widths = append(widths, 36, 28, 36, 22)
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for i := range row {
			row[i] = "-"
		}
		rows = append(rows, row)
	}
	printListTable(base, headers, rows, widths, tableOptions{}, long)
	fmt.Fprintf(os.Stdout, "\n共 %d 个用户组。\n", len(items))
}

func PrintGroupGetSummary(results []*service.GroupGetResult, long bool) {
	if captureJSON(results) {
		return
	}
	for index, result := range results {
		if index > 0 {
			fmt.Fprintln(os.Stdout)
		}
		PrintGroupGetResult(result, long)
	}
}

func printAuthGroupMembers(result *service.GroupGetResult, long bool) {
	if result == nil {
		return
	}
	fmt.Fprintln(os.Stdout, "\n成员：")
	rows := make([][]string, 0, maxInt(1, len(result.Members)))
	for _, member := range result.Members {
		row := []string{
			emptyDash(member.Username), emptyDash(member.Name), emptyDash(member.Role),
			emptyDash(member.Status), emptyDash(member.JoinTime),
		}
		if long {
			inGroup := "否"
			if member.InGroup {
				inGroup = "是"
			}
			row = append([]string{emptyDash(member.ID)}, row...)
			row = append(row, inGroup)
		}
		rows = append(rows, row)
	}
	headers := []string{"USERNAME", "NAME", "ROLE", "STATUS", "JOINED"}
	widths := []int{24, 18, 12, 10, 19}
	minimums := []int{12, 10, 8, 8, 19}
	if long {
		headers = append([]string{"ID"}, headers...)
		headers = append(headers, "IN GROUP")
		widths = append([]int{38}, widths...)
		widths = append(widths, 10)
		minimums = append([]int{36}, minimums...)
		minimums = append(minimums, 8)
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for index := range row {
			row[index] = "-"
		}
		rows = append(rows, row)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{minWidths: minimums})
	fmt.Fprintf(os.Stdout, "\n成员 %d 人，其中协管员 %d 人。\n", result.MemberCount, result.AdminCount)
}
