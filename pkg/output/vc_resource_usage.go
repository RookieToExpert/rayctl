package output

import (
	"strings"
	"unicode"

	"rayctl/internal/service"
)

func PrintVCResourceUsage(results []*service.VCResourceUsageResult) {
	multipleVCs := len(results) > 1
	rows := make([][]string, 0)
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			usage := item.Usage.Usage
			row := []string{
				emptyDash(item.HostName),
				emptyDash(item.HostIP),
				emptyDash(item.State),
				formatPlatformResourcePair(usage.Allocated.Device, usage.Total.Device),
				formatPlatformResourcePair(usage.Allocated.CPU, usage.Total.CPU),
				formatPlatformResourcePair(usage.Allocated.Memory, usage.Total.Memory),
			}
			if multipleVCs {
				row = append([]string{emptyDash(result.ClusterName)}, row...)
			}
			rows = append(rows, row)
		}
	}
	headers := []string{"HOST", "IP", "STATE", "ACCEL ALLOC/TOTAL", "CPU ALLOC/TOTAL", "MEMORY ALLOC/TOTAL"}
	maxWidths := []int{20, 15, 10, 18, 18, 22}
	minWidths := []int{20, 15, 8, 17, 15, 20}
	emptyRow := []string{"-", "-", "-", "-", "-", "-"}
	if multipleVCs {
		headers = append([]string{"VC"}, headers...)
		maxWidths = append([]int{28}, maxWidths...)
		minWidths = append([]int{16}, minWidths...)
		emptyRow = append([]string{"-"}, emptyRow...)
	}
	if len(rows) == 0 {
		rows = append(rows, emptyRow)
	}
	printBoxTableWithOptions(headers, rows, maxWidths, tableOptions{minWidths: minWidths})
}

func formatPlatformResourcePair(allocated string, total string) string {
	allocatedNumber, allocatedUnit := splitPlatformResourceAmount(allocated)
	totalNumber, totalUnit := splitPlatformResourceAmount(total)
	if allocatedNumber == "" && totalNumber == "" {
		return "-"
	}
	if allocatedNumber == "" || totalNumber == "" {
		return emptyDash(strings.Join([]string{strings.TrimSpace(allocated), strings.TrimSpace(total)}, "/"))
	}
	unit := totalUnit
	if unit == "" {
		unit = allocatedUnit
	}
	return allocatedNumber + "/" + totalNumber + unit
}

func splitPlatformResourceAmount(value string) (string, string) {
	value = strings.TrimSpace(value)
	index := strings.IndexFunc(value, func(char rune) bool {
		return !(unicode.IsDigit(char) || char == '.' || char == '-')
	})
	if index < 0 {
		return value, ""
	}
	return strings.TrimSpace(value[:index]), strings.TrimSpace(value[index:])
}
