package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	metricsquery "rayctl/internal/metrics"
)

func PrintMetrics(result *metricsquery.Result, format string) error {
	if result == nil {
		return fmt.Errorf("metrics result is empty")
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table":
		printMetricsTable(result)
		return nil
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	case "csv":
		return writeMetricsCSV(os.Stdout, result)
	default:
		return fmt.Errorf("unsupported output %q; expected table, json, or csv", format)
	}
}

func printMetricsTable(result *metricsquery.Result) {
	metadataRows := [][]string{
		{"WORKLOAD", emptyDash(result.Metadata.Workload)},
		{"TYPE", emptyDash(result.Metadata.WorkloadType)},
		{"WORKSPACE", emptyDash(result.Metadata.Workspace)},
		{"PODS", emptyDash(strings.Join(result.Metadata.Pods, ", "))},
		{"RANGE", metricsRange(result.Metadata)},
		{"SAMPLES", fmt.Sprintf("%s | interval %s", metricsMode(result.Metadata), emptyDash(result.Metadata.SampleInterval))},
	}
	if result.Metadata.Command == "gpu" {
		metadataRows = append(metadataRows, []string{"HARDWARE", emptyDash(strings.Join(metricsHardwareTypes(result), ", "))})
	}
	if result.Metadata.Command == "raw" {
		metadataRows = [][]string{
			{"QUERY", result.Metadata.Labels["query"]},
			{"RANGE", metricsRange(result.Metadata)},
			{"SAMPLES", fmt.Sprintf("%s | interval %s", metricsMode(result.Metadata), emptyDash(result.Metadata.SampleInterval))},
		}
	}
	printBoxTableWithOptions([]string{"FIELD", "VALUE"}, metadataRows, []int{12, 100}, tableOptions{minWidths: []int{10, 32}})
	fmt.Fprintln(os.Stdout)

	switch result.Metadata.Command {
	case "mem":
		printMemorySamples(result)
	case "gpu":
		printGPUSamples(result)
	case "restart":
		printRestartSamples(result)
	default:
		printRawMetricSamples(result)
	}
	if len(result.Warnings) > 0 {
		fmt.Fprintln(os.Stdout)
		for _, warning := range result.Warnings {
			fmt.Fprintf(os.Stdout, "Warning: %s\n", warning)
		}
	}
}

func printMemorySamples(result *metricsquery.Result) {
	series := metricsSeriesMap(result)
	rows := make([][]string, 0, len(result.Samples))
	for _, sample := range result.Samples {
		item := series[sample.Series]
		percent := sample.Percent
		percentText := "-"
		if item.Limit > 0 {
			percentText = fmt.Sprintf("%.0f%%", percent)
		}
		rows = append(rows, []string{
			metricsClock(sample.Timestamp), sample.Series, formatGiB(sample.Value), percentText, metricsBar(percent),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"TIME", "POD/CONTAINER", "MEMORY", "LIMIT %", "USAGE"}, rows,
		[]int{18, 48, 12, 9, 30}, tableOptions{minWidths: []int{14, 18, 10, 8, 16}},
	)
	printMetricSummary(result, "MEMORY")
}

func printGPUSamples(result *metricsquery.Result) {
	rows := make([][]string, 0, len(result.Samples))
	for _, sample := range result.Samples {
		percent := sample.Percent
		rows = append(rows, []string{
			metricsClock(sample.Timestamp), sample.Series, fmt.Sprintf("%.1f%%", percent), metricsBar(percent),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"TIME", "POD/CONTAINER/GPU", "UTIL", "USAGE"}, rows,
		[]int{18, 54, 10, 30}, tableOptions{minWidths: []int{14, 18, 8, 16}},
	)
	printMetricSummary(result, "UTIL")
}

func printRestartSamples(result *metricsquery.Result) {
	rows := make([][]string, 0, len(result.Samples))
	for _, sample := range result.Samples {
		rows = append(rows, []string{
			metricsClock(sample.Timestamp), sample.Series, emptyDash(sample.Event), formatNumber(sample.Delta), emptyDash(sample.Reason), emptyDash(sample.UID),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "0", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"TIME", "POD/CONTAINER", "EVENT", "RESTARTS", "REASON", "POD UID"}, rows,
		[]int{18, 44, 14, 10, 20, 36}, tableOptions{minWidths: []int{14, 18, 10, 8, 10, 16}},
	)
	fmt.Fprintf(os.Stdout, "\nRestart events: %d\n", result.Summary.EventCount)
}

func printRawMetricSamples(result *metricsquery.Result) {
	rows := make([][]string, 0, len(result.Samples))
	for _, sample := range result.Samples {
		rows = append(rows, []string{metricsClock(sample.Timestamp), sample.Series, formatNumber(sample.Value)})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-"})
	}
	printBoxTableWithOptions([]string{"TIME", "SERIES", "VALUE"}, rows, []int{18, 88, 20}, tableOptions{minWidths: []int{14, 24, 10}})
	fmt.Fprintf(os.Stdout, "\nSamples: %d\n", result.Summary.SampleCount)
}

func printMetricSummary(result *metricsquery.Result, valueLabel string) {
	if len(result.Series) == 0 {
		return
	}
	rows := make([][]string, 0, len(result.Series))
	seriesByName := metricsSeriesMap(result)
	for _, series := range result.Series {
		item := series.Summary
		peak := formatMetricValue(result.Metadata.Command, item.Peak)
		latest := formatMetricValue(result.Metadata.Command, item.Latest)
		limit := "-"
		if series.Limit > 0 {
			limit = formatMetricValue(result.Metadata.Command, series.Limit)
		}
		row := []string{series.Name, peak, metricsClock(item.PeakAt), latest, limit, strconv.Itoa(item.SampleCount)}
		if result.Metadata.Command == "gpu" {
			detail := seriesByName[series.Name]
			memory := "-"
			if detail.MemoryTotalMiB > 0 {
				memory = fmt.Sprintf("%.0f/%.0f MiB", detail.MemoryUsedPeakMiB, detail.MemoryTotalMiB)
			}
			row = append(row, memory)
		}
		rows = append(rows, row)
	}
	fmt.Fprintln(os.Stdout)
	headers := []string{"SERIES", "PEAK " + valueLabel, "PEAK TIME", "LATEST", "LIMIT", "SAMPLES"}
	widths := []int{48, 14, 18, 14, 14, 9}
	minimums := []int{18, 10, 14, 10, 10, 8}
	if result.Metadata.Command == "gpu" {
		headers = append(headers, "VRAM PEAK/TOTAL")
		widths = append(widths, 22)
		minimums = append(minimums, 14)
	}
	printBoxTableWithOptions(
		headers, rows, widths, tableOptions{minWidths: minimums},
	)
}

func writeMetricsCSV(writer io.Writer, result *metricsquery.Result) error {
	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()
	header := []string{"time", "series", "value"}
	if result.Metadata.Command == "restart" {
		header = []string{"time", "series", "restarts", "total", "reason", "uid", "event"}
	}
	if err := csvWriter.Write(header); err != nil {
		return err
	}
	for _, sample := range result.Samples {
		row := []string{sample.Timestamp, sample.Series, strconv.FormatFloat(sample.Value, 'g', -1, 64)}
		if result.Metadata.Command == "restart" {
			row = []string{
				sample.Timestamp,
				sample.Series,
				strconv.FormatFloat(sample.Delta, 'g', -1, 64),
				strconv.FormatFloat(sample.Value, 'g', -1, 64),
				sample.Reason,
				sample.UID,
				sample.Event,
			}
		}
		if err := csvWriter.Write(row); err != nil {
			return err
		}
	}
	return csvWriter.Error()
}

func metricsSeriesMap(result *metricsquery.Result) map[string]metricsquery.Series {
	items := make(map[string]metricsquery.Series, len(result.Series))
	for _, item := range result.Series {
		items[item.Name] = item
	}
	return items
}

func metricsMode(metadata metricsquery.Metadata) string {
	if metadata.Mode == "step" {
		step := ""
		if metadata.Step != nil {
			step = *metadata.Step
		}
		return "步长查询 (step=" + emptyDash(step) + ")"
	}
	if metadata.Mode == "instant" {
		return "瞬时查询"
	}
	return "原始采样点"
}

func metricsRange(metadata metricsquery.Metadata) string {
	return fmt.Sprintf("%s ~ %s (%s)", metricsClock(metadata.Start), metricsClock(metadata.End), metadata.Timezone)
}

func metricsClock(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return emptyDash(value)
	}
	return parsed.Format("01-02 15:04:05")
}

func formatMetricValue(command string, value float64) string {
	switch command {
	case "mem":
		return formatGiB(value)
	case "gpu":
		return fmt.Sprintf("%.1f%%", value)
	default:
		return formatNumber(value)
	}
}

func formatGiB(bytes float64) string {
	return fmt.Sprintf("%.2f GiB", bytes/(1024*1024*1024))
}

func formatNumber(value float64) string {
	if math.Abs(value-math.Round(value)) < 1e-9 {
		return strconv.FormatInt(int64(math.Round(value)), 10)
	}
	return strconv.FormatFloat(value, 'f', 3, 64)
}

func metricsBar(percent float64) string {
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	const width = 24
	filled := int(math.Round(percent / 100 * width))
	return strings.Repeat("#", filled) + strings.Repeat(".", width-filled)
}

func sortedMetricSeriesNames(result *metricsquery.Result) []string {
	names := make([]string, 0, len(result.Series))
	for _, item := range result.Series {
		names = append(names, item.Name)
	}
	sort.Strings(names)
	return names
}

func metricsHardwareTypes(result *metricsquery.Result) []string {
	set := map[string]struct{}{}
	for _, item := range result.Series {
		if value := strings.TrimSpace(item.Hardware); value != "" {
			set[value] = struct{}{}
		}
	}
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
