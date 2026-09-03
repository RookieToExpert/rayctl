package output

import (
	"strconv"
	"strings"
	"unicode"

	"rayctl/internal/service"
)

func PrintVCResourceUsage(results []*service.VCResourceUsageResult) {
	multipleVCs := len(results) > 1
	showAccelerator := vcUsageHasAccelerator(results)
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
			}
			if showAccelerator {
				row = append(row, formatPlatformResourcePair(usage.Allocated.Device, usage.Total.Device))
			}
			row = append(row,
				formatPlatformResourcePair(usage.Allocated.CPU, usage.Total.CPU),
				formatPlatformResourcePair(usage.Allocated.Memory, usage.Total.Memory),
			)
			if multipleVCs {
				row = append([]string{emptyDash(result.ClusterName)}, row...)
			}
			rows = append(rows, row)
		}
	}
	headers := []string{"HOST", "IP", "STATE"}
	maxWidths := []int{20, 15, 10}
	minWidths := []int{20, 15, 8}
	emptyRow := []string{"-", "-", "-"}
	if showAccelerator {
		headers = append(headers, "ACCEL ALLOC/TOTAL")
		maxWidths = append(maxWidths, 18)
		minWidths = append(minWidths, 17)
		emptyRow = append(emptyRow, "-")
	}
	headers = append(headers, "CPU ALLOC/TOTAL", "MEMORY ALLOC/TOTAL")
	maxWidths = append(maxWidths, 18, 22)
	minWidths = append(minWidths, 15, 20)
	emptyRow = append(emptyRow, "-", "-")
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

func vcUsageHasAccelerator(results []*service.VCResourceUsageResult) bool {
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, item := range result.Items {
			usage := item.Usage.Usage
			if platformResourceAmountIsPositive(usage.Total.Device) ||
				platformResourceAmountIsPositive(usage.Allocated.Device) ||
				platformResourceAmountIsPositive(usage.Available.Device) {
				return true
			}
		}
	}
	return false
}

func platformResourceAmountIsPositive(value string) bool {
	number, _ := splitPlatformResourceAmount(value)
	parsed, err := strconv.ParseFloat(number, 64)
	return err == nil && parsed > 0
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
