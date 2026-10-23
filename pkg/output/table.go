package output

import (
	"fmt"
	"golang.org/x/term"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type tableOptions struct {
	noWrapCells map[string]struct{}
	minWidths   []int
	rowDividers bool
	cellColors  map[string]string
}

func cellKey(row int, col int) string {
	return fmt.Sprintf("%d:%d", row, col)
}

func makeNoWrapCells(cells ...[2]int) map[string]struct{} {
	if len(cells) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(cells))
	for _, cell := range cells {
		result[cellKey(cell[0], cell[1])] = struct{}{}
	}
	return result
}

func noWrapCellsForSingleColumn(rowCount int, col int) [][2]int {
	if rowCount <= 0 {
		return nil
	}
	result := make([][2]int, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		result = append(result, [2]int{i, col})
	}
	return result
}

func noWrapCellsForColumns(rowCount int, columns ...int) [][2]int {
	cells := make([][2]int, 0, rowCount*len(columns))
	for _, column := range columns {
		cells = append(cells, noWrapCellsForSingleColumn(rowCount, column)...)
	}
	return cells
}

func printResourceDetail(rows [][]string) {
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{18, 100},
		tableOptions{minWidths: []int{12, 24}},
	)
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func yesNo(value bool) string {
	if value {
		return "Y"
	}
	return "N"
}

func yesNoFromReady(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ready", "true", "yes", "y":
		return "Y"
	default:
		return "N"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func printBoxTable(headers []string, rows [][]string) {
	printBoxTableWithOptions(headers, rows, nil, tableOptions{})
}

func printBoxTableWithMaxWidths(headers []string, rows [][]string, maxWidths []int) {
	printBoxTableWithOptions(headers, rows, maxWidths, tableOptions{})
}

func printBoxTableWithOptions(headers []string, rows [][]string, maxWidths []int, options tableOptions) {
	options = preserveTableIdentifiers(headers, rows, options)
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = textWidth(header)
		if i < len(maxWidths) && maxWidths[i] > 0 && widths[i] > maxWidths[i] {
			widths[i] = maxWidths[i]
		}
	}

	for rowIdx, row := range rows {
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			cellLen := 0
			for _, line := range strings.Split(cell, "\n") {
				cellLen = max(cellLen, textWidth(line))
			}
			if !noWrapCell(options, rowIdx, i) && i < len(maxWidths) && maxWidths[i] > 0 && cellLen > maxWidths[i] {
				cellLen = maxWidths[i]
			}
			if cellLen > widths[i] {
				widths[i] = cellLen
			}
		}
	}

	fitWidthsToTerminal(widths, headers, options)

	fmt.Fprintln(os.Stdout, borderLine("┌", "┬", "┐", widths))
	fmt.Fprintln(os.Stdout, renderRow(headers, widths))
	fmt.Fprintln(os.Stdout, borderLine("├", "┼", "┤", widths))
	for rowIdx, row := range rows {
		for _, rendered := range renderWrappedRows(rowIdx, row, widths, options) {
			fmt.Fprintln(os.Stdout, rendered)
		}
		if options.rowDividers && rowIdx < len(rows)-1 {
			fmt.Fprintln(os.Stdout, borderLine("├", "┼", "┤", widths))
		}
	}
	fmt.Fprintln(os.Stdout, borderLine("└", "┴", "┘", widths))
}

func fitWidthsToTerminal(widths []int, headers []string, options tableOptions) {
	if len(widths) == 0 {
		return
	}

	maxTotal := terminalWidth() - 1
	if maxTotal < 40 {
		maxTotal = 40
	}

	currentTotal := tableDisplayWidth(widths)
	if currentTotal <= maxTotal {
		return
	}

	minWidths := make([]int, len(widths))
	for i := range widths {
		minWidth := 8
		if i < len(headers) {
			headerWidth := textWidth(headers[i])
			if headerWidth > minWidth {
				minWidth = headerWidth
			}
		}
		if len(widths) == 1 {
			minWidth = 20
		}
		if i < len(options.minWidths) && options.minWidths[i] > minWidth {
			minWidth = options.minWidths[i]
		}
		minWidths[i] = minWidth
	}

	for currentTotal > maxTotal {
		widest := -1
		widestSpare := 0
		for i := range widths {
			spare := widths[i] - minWidths[i]
			if spare > widestSpare {
				widest = i
				widestSpare = spare
			}
		}
		if widest == -1 || widestSpare <= 0 {
			break
		}
		widths[widest]--
		currentTotal = tableDisplayWidth(widths)
	}
}

func tableDisplayWidth(widths []int) int {
	total := 1
	for _, width := range widths {
		total += width + 3
	}
	return total
}

func borderLine(left string, middle string, right string, widths []int) string {
	var b strings.Builder
	b.WriteString(left)
	for i, width := range widths {
		b.WriteString(strings.Repeat("─", width+2))
		if i == len(widths)-1 {
			b.WriteString(right)
		} else {
			b.WriteString(middle)
		}
	}
	return b.String()
}

func renderRow(row []string, widths []int) string {
	return renderColoredRow(row, widths, nil)
}

func renderColoredRow(row []string, widths []int, colors map[int]string) string {
	var b strings.Builder
	b.WriteString("│")
	for i, width := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		cell = truncateCell(cell, width)
		padding := width - textWidth(cell)
		if padding < 0 {
			padding = 0
		}
		b.WriteString(" ")
		if colors[i] != "" {
			b.WriteString(colors[i])
		}
		b.WriteString(cell)
		if colors[i] != "" {
			b.WriteString("\x1b[0m")
		}
		b.WriteString(strings.Repeat(" ", padding+1))
		b.WriteString("│")
	}
	return b.String()
}

func renderWrappedRows(rowIdx int, row []string, widths []int, options tableOptions) []string {
	cellLines := make([][]string, len(widths))
	maxLines := 1
	for i, width := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		lines := wrapCell(cell, width, noWrapCell(options, rowIdx, i))
		cellLines[i] = lines
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}

	rendered := make([]string, 0, maxLines)
	for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
		current := make([]string, len(widths))
		colors := map[int]string{}
		for cellIdx := range widths {
			if lineIdx < len(cellLines[cellIdx]) {
				current[cellIdx] = cellLines[cellIdx][lineIdx]
			}
			if color := options.cellColors[cellKey(rowIdx, cellIdx)]; color != "" {
				colors[cellIdx] = color
			}
		}
		rendered = append(rendered, renderColoredRow(current, widths, colors))
	}
	return rendered
}

func noWrapCell(options tableOptions, row int, col int) bool {
	if len(options.noWrapCells) == 0 {
		return false
	}
	_, ok := options.noWrapCells[cellKey(row, col)]
	return ok
}

func wrapCell(value string, width int, noWrap bool) []string {
	if width <= 0 {
		return []string{value}
	}
	if value == "" {
		return []string{""}
	}
	if noWrap {
		paragraphs := strings.Split(value, "\n")
		lines := make([]string, 0, len(paragraphs))
		for _, paragraph := range paragraphs {
			lines = append(lines, strings.TrimRight(paragraph, " "))
		}
		return lines
	}

	paragraphs := strings.Split(value, "\n")
	lines := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimRight(paragraph, " ")
		runes := []rune(paragraph)
		if len(runes) == 0 {
			lines = append(lines, "")
			continue
		}
		for textWidth(string(runes)) > width {
			split := bestWrapPosition(runes, width)
			lines = append(lines, strings.TrimSpace(string(runes[:split])))
			runes = trimLeadingWrapRunes(runes[split:])
		}
		lines = append(lines, string(runes))
	}
	return lines
}

func bestWrapPosition(runes []rune, width int) int {
	if textWidth(string(runes)) <= width {
		return len(runes)
	}

	best := prefixWithinWidth(runes, width)
	for i := best; i > 0; i-- {
		if isWrapBoundary(runes[i-1]) {
			best = i
			break
		}
	}
	if best <= 0 {
		best = width
	}
	return best
}

func trimLeadingWrapRunes(runes []rune) []rune {
	start := 0
	for start < len(runes) {
		if runes[start] != ' ' {
			break
		}
		start++
	}
	return runes[start:]
}

func isWrapBoundary(ch rune) bool {
	switch ch {
	case ' ', ',', '.', ';', ':', '|', '/', '，', '。', '；', '：', '、', '）', ')', ']', '】', '>':
		return true
	default:
		return false
	}
}

func truncateCell(value string, width int) string {
	if width <= 0 || textWidth(value) <= width {
		return value
	}
	if width <= 1 {
		return "."
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}

	runes := []rune(value)
	return string(runes[:prefixWithinWidth(runes, width-3)]) + "..."
}

func formatRepairFlag(v bool) string {
	return fmt.Sprintf("%t", v)
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}

func firstNonEmptyOutput(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func joinLinesOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, "\n")
}

func formatOptionalDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	return d.String()
}

func terminalWidth() int {
	for _, fd := range []uintptr{os.Stdout.Fd(), os.Stderr.Fd(), os.Stdin.Fd()} {
		if width, _, err := term.GetSize(int(fd)); err == nil && width >= 40 {
			return width
		}
	}
	if value := strings.TrimSpace(os.Getenv("COLUMNS")); value != "" {
		if width, err := strconv.Atoi(value); err == nil && width >= 40 && width <= 400 {
			return width
		}
	}
	return 100
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncateForWidth(value string, width int) string {
	if width <= 0 {
		return value
	}
	return truncateCell(value, width)
}

func textWidth(value string) int {
	if value == "" {
		return 0
	}
	width := 0
	for _, r := range value {
		width += runeDisplayWidth(r)
	}
	return width
}

func prefixWithinWidth(runes []rune, maxWidth int) int {
	if maxWidth <= 0 {
		return 0
	}
	currentWidth := 0
	for i, r := range runes {
		rw := runeDisplayWidth(r)
		if currentWidth+rw > maxWidth {
			return i
		}
		currentWidth += rw
	}
	return len(runes)
}

func runeDisplayWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 32 || (r >= 0x7f && r < 0xa0):
		return 0
	case unicode.In(r,
		unicode.Han,
		unicode.Hangul,
		unicode.Hiragana,
		unicode.Katakana):
		return 2
	case isFullWidthRune(r):
		return 2
	default:
		return 1
	}
}

func isFullWidthRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F:
		return true
	case r >= 0x2329 && r <= 0x232A:
		return true
	case r >= 0x2E80 && r <= 0xA4CF:
		return true
	case r >= 0xAC00 && r <= 0xD7A3:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0xFE10 && r <= 0xFE19:
		return true
	case r >= 0xFE30 && r <= 0xFE6F:
		return true
	case r >= 0xFF00 && r <= 0xFF60:
		return true
	case r >= 0xFFE0 && r <= 0xFFE6:
		return true
	default:
		return false
	}
}
