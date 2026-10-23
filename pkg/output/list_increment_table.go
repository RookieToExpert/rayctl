package output

import "strings"

type listIdentity struct {
	Scope string
	UIDs  []string
}

// Projection happens after JSON capture and only copies display rows. Keep
// backend ordering and values, with one identity plus fields absent by default.
func printListTable(base, headers []string, rows [][]string, widths []int, options tableOptions, incremental bool, identities ...listIdentity) {
	if incremental {
		columns := []int{0}
		if len(identities) > 0 {
			identity := identities[0]
			scope := -1
			for col, header := range headers {
				if header == identity.Scope {
					scope = col
				}
			}
			seen, pairs := map[string]bool{}, map[string]bool{}
			duplicate, pairDuplicate := false, false
			for _, row := range rows {
				name := listCell(row, 0)
				if name == "-" {
					continue
				}
				duplicate = duplicate || seen[name]
				seen[name] = true
				key := name + "\x00" + listCell(row, scope)
				pairDuplicate = pairDuplicate || pairs[key]
				pairs[key] = true
			}
			if duplicate && scope > 0 && !pairDuplicate {
				columns = append(columns, scope)
			} else if duplicate && len(identity.UIDs) == len(rows) {
				// Same name and workspace can still exist in two subscriptions.
				columns = append(columns, len(headers))
				headers = append(append([]string{}, headers...), "UID")
				widths = append(append([]int{}, widths...), 36)
				minimums := make([]int, len(headers)-1)
				copy(minimums, options.minWidths)
				options.minWidths = append(minimums, 36)
				copied := make([][]string, len(rows))
				for r, row := range rows {
					copied[r] = append(append([]string{}, row...), emptyDash(identity.UIDs[r]))
				}
				rows = copied
			}
		}
		defaultColumns := map[string]bool{}
		for _, header := range base {
			defaultColumns[header] = true
		}
		for col, header := range headers {
			if defaultColumns[header] {
				continue
			}
			already := false
			for _, selected := range columns {
				already = already || selected == col
			}
			if !already {
				columns = append(columns, col)
			}
		}
		projectedHeaders, projectedWidths := []string{}, []int{}
		projectedRows := make([][]string, len(rows))
		projectedOptions := tableOptions{rowDividers: options.rowDividers, noWrapCells: map[string]struct{}{}, cellColors: map[string]string{}}
		for c, old := range columns {
			projectedHeaders = append(projectedHeaders, headers[old])
			projectedWidths = append(projectedWidths, listWidth(widths, old))
			projectedOptions.minWidths = append(projectedOptions.minWidths, listWidth(options.minWidths, old))
			for r, row := range rows {
				projectedRows[r] = append(projectedRows[r], listCell(row, old))
				if _, ok := options.noWrapCells[cellKey(r, old)]; ok {
					projectedOptions.noWrapCells[cellKey(r, c)] = struct{}{}
				}
				if color := options.cellColors[cellKey(r, old)]; color != "" {
					projectedOptions.cellColors[cellKey(r, c)] = color
				}
			}
		}
		headers, rows, widths, options = projectedHeaders, projectedRows, projectedWidths, projectedOptions
	}
	protected := map[string]struct{}{}
	for key := range options.noWrapCells {
		protected[key] = struct{}{}
	}
	for col, header := range headers {
		if col == 0 || listHeaderNoWrap(header) {
			for row := range rows {
				protected[cellKey(row, col)] = struct{}{}
			}
		}
	}
	options.noWrapCells = protected
	printBoxTableWithOptions(headers, rows, widths, options)
}

func listCell(row []string, col int) string {
	if col < 0 || col >= len(row) || row[col] == "" {
		return "-"
	}
	return row[col]
}

func listWidth(widths []int, col int) int {
	if col < len(widths) {
		return widths[col]
	}
	return 0
}

func listHeaderNoWrap(header string) bool {
	if identifierHeader(header) {
		return true
	}
	switch strings.ToUpper(header) {
	case "CREATED", "UPDATED", "FAILED", "JOINED", "TENANT", "TENANT CODE", "TENANT ID", "POSIX", "ACN", "MODEL", "MACHINE", "MACHINE TYPE", "PROD", "VPC", "SUBNET", "NATGW":
		return true
	}
	return false
}
