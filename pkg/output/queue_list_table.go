package output

import "rayctl/internal/service"

func printQueueNodeListItems(items []service.VCNodeListItem, long bool) {
	base := []string{"HOST", "IP", "STATE", "MACHINE TYPE", "MODEL"}
	headers := append([]string{}, base...)
	widths := []int{20, 15, 8, 14, 18}
	if long {
		headers = append(headers, "ACN UID")
		widths = append(widths, 36)
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{emptyDash(item.HostName), emptyDash(item.HostIP), emptyDash(item.State), emptyDash(item.MachineType), emptyDash(item.Model)}
		if long {
			row = append(row, emptyDash(item.UID))
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		row := make([]string, len(headers))
		for c := range row {
			row[c] = "-"
		}
		rows = append(rows, row)
	}
	printListTable(base, headers, rows, widths, tableOptions{}, long)
}
