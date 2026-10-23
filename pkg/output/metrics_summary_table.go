package output

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	metricsquery "rayctl/internal/metrics"
)

func metricsMemory(value *float64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%.1fGiB", *value)
}

func metricsOptionalPercent(value *float64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", *value)
}

func metricsOptionalNumber(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'g', -1, 64)
}

func printMetricsAggregate(result *metricsquery.Result) {
	headers := []string{"SERIES", "AVG", "P95", "MAX", "NONZERO", "LAST"}
	widths := []int{54, 13, 13, 13, 10, 13}
	minimums := []int{20, 9, 9, 9, 9, 9}
	if result.Metadata.Command == "gpu" {
		headers = append(headers, "MEM", "MEM%")
		widths = append(widths, 13, 10)
		minimums = append(minimums, 10, 8)
	}
	rows := [][]string{}
	for _, series := range result.Series {
		summary := series.Summary
		row := []string{series.Name, formatMetricValue(result.Metadata.Command, summary.Average), formatMetricValue(result.Metadata.Command, summary.P95), formatMetricValue(result.Metadata.Command, summary.Peak), fmt.Sprintf("%.1f%%", summary.NonzeroPercent), formatMetricValue(result.Metadata.Command, summary.Latest)}
		if result.Metadata.Command == "gpu" {
			row = append(row, metricsMemory(series.MemoryUsedGiB), metricsOptionalPercent(series.MemoryPercent))
		}
		rows = append(rows, row)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{minWidths: minimums})
	for _, capability := range result.Capabilities {
		keys := make([]string, 0, len(capability.Dimensions))
		for key := range capability.Dimensions {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := []string{}
		for _, key := range keys {
			parts = append(parts, key+"="+capability.Dimensions[key])
		}
		fmt.Fprintf(os.Stdout, "%s: %s\n", capability.Hardware, strings.Join(parts, ", "))
	}
}

func printGPUHealth(result *metricsquery.Result) {
	health := result.Health
	if health == nil {
		return
	}
	fmt.Fprintf(os.Stdout, "\n健康指标: %d 张卡，%d 张卡无错误计数，%d 张异常，%d 张健康指标无数据\n", health.TotalCards, health.HealthyCards, len(health.Anomalies), health.UnknownCards)
	if len(health.Information) > 0 {
		rows := [][]string{}
		for _, card := range health.Information {
			rows = append(rows, []string{card.Node, card.GPU, card.UUID, card.Pod, healthNumber(card.ECCSingleBit)})
		}
		fmt.Fprintln(os.Stdout, "SBE 累计计数（信息项，不单独判异常）：")
		printBoxTable([]string{"NODE", "GPU", "UUID", "POD", "SBE"}, rows)
	}
	if len(health.Anomalies) == 0 {
		return
	}
	rows := [][]string{}
	for _, card := range health.Anomalies {
		rows = append(rows, []string{card.Node, card.GPU, emptyDash(card.UUID), emptyDash(card.Model), card.Pod, healthNumber(card.RAS), healthNumber(card.ECC), healthNumber(card.ErrorCode), healthNumber(card.IsolatedPages), "有错误计数"})
	}
	printBoxTableWithOptions([]string{"NODE", "GPU", "UUID", "MODEL", "POD", "RAS", "ECC", "ERROR CODE", "ISOLATED", "结论"}, rows, []int{28, 8, 42, 18, 48, 10, 10, 14, 12, 14}, tableOptions{})
}

func healthNumber(value *float64) string {
	if value == nil {
		return "无数据"
	}
	return formatNumber(*value)
}

func printGPUBaseline(result *metricsquery.Result) {
	b := result.Baseline
	if b == nil {
		return
	}
	fmt.Fprintf(os.Stdout, "\nBASELINE 全 %d 副本 UTIL p50=%.1f%% p95=%.1f%% max=%.1f%% | MEM p50=%s p95=%s min=%s\n", b.PodCount, b.UtilP50, b.UtilP95, b.UtilMax, metricsMemory(b.MemoryP50GiB), metricsMemory(b.MemoryP95GiB), metricsMemory(b.MemoryMinGiB))
	if b.SelectedMemoryPercentile != nil {
		fmt.Fprintf(os.Stdout, "所选 Pod 的 MEM 最低位于全体 p%.1f", *b.SelectedMemoryPercentile)
		if b.SignificantlyLow {
			fmt.Fprint(os.Stdout, "，显著偏低")
		}
		fmt.Fprintln(os.Stdout)
	}
}

func writeMetricsAggregateCSV(writer io.Writer, result *metricsquery.Result) error {
	w := csv.NewWriter(writer)
	headers := []string{"series", "avg", "p95", "max", "nonzero_percent", "last", "sample_count"}
	if result.Metadata.Command == "gpu" {
		headers = append(headers, "memory_used_gib", "memory_total_gib", "memory_percent")
	}
	if err := w.Write(headers); err != nil {
		return err
	}
	for _, series := range result.Series {
		s := series.Summary
		row := []string{series.Name, formatNumber(s.Average), formatNumber(s.P95), formatNumber(s.Peak), formatNumber(s.NonzeroPercent), formatNumber(s.Latest), strconv.Itoa(s.SampleCount)}
		if result.Metadata.Command == "gpu" {
			row = append(row, metricsOptionalNumber(series.MemoryUsedGiB), metricsOptionalNumber(series.MemoryTotalGiB), metricsOptionalNumber(series.MemoryPercent))
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
