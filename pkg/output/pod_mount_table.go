package output

// The volume name is an alias; do not print a long PVC identifier twice when
// it is also the source. Distinct aliases, mount paths and access flags stay.
func printPodMountTable(rows [][]string) {
	if len(rows) == 0 {
		return
	}
	headers := []string{"CONTAINER", "VOLUME", "TYPE", "SOURCE", "MOUNT", "SUBPATH", "READ ONLY", "PVC STATUS", "AFS/AOSS ENDPOINT"}
	for _, row := range rows {
		if row[1] == row[3] && row[3] != "" && row[3] != "-" {
			row[1] = "-"
		}
	}
	optional := map[int]bool{1: true, 3: true, 5: true, 7: true, 8: true}
	columns := []int{}
	visibleHeaders := []string{}
	typeColumn := 0
	for col, header := range headers {
		visible := !optional[col]
		for _, row := range rows {
			if row[col] != "" && row[col] != "-" {
				visible = true
				break
			}
		}
		if visible {
			if col == 2 {
				typeColumn = len(columns)
			}
			columns = append(columns, col)
			visibleHeaders = append(visibleHeaders, header)
		}
	}
	visibleRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		values := make([]string, 0, len(columns))
		for _, col := range columns {
			values = append(values, row[col])
		}
		visibleRows = append(visibleRows, values)
	}
	printBoxTableWithOptions(visibleHeaders, visibleRows, nil, tableOptions{noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), typeColumn)...)})
}
