package output

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	metricsquery "rayctl/internal/metrics"
)

func TestMetricsDefaultSummaryAndDetailMemory(t *testing.T) {
	mem, pct := .8, 1.25
	r := &metricsquery.Result{Metadata: metricsquery.Metadata{Command: "gpu"}, Series: []metricsquery.Series{{Name: "p/gpu=1", MemoryUsedGiB: &mem, MemoryPercent: &pct, Summary: metricsquery.SeriesSummary{Average: 10, P95: 19, Peak: 20, Latest: 20, NonzeroPercent: 50, SampleCount: 2}}}, Samples: []metricsquery.Sample{{Series: "p/gpu=1", Value: 0, MemoryUsedGiB: &mem, MemoryPercent: &pct}, {Series: "p/gpu=1", Value: 20, MemoryUsedGiB: &mem, MemoryPercent: &pct}}}
	text := captureTableOutput(t, func() { printMetricsTable(r) })
	for _, want := range []string{"AVG", "P95", "NONZERO", "LAST", "MEM%", "0.8GiB"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	if strings.Contains(text, "PEAK TIME") || strings.Contains(text, "TIME ") {
		t.Fatal("default prints detail", text)
	}
	var b bytes.Buffer
	if err := writeMetricsCSV(&b, r); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&b).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][7] != "0.8" {
		t.Fatal(b.String(), err)
	}
	r.Metadata.Detail = true
	b.Reset()
	if err := writeMetricsCSV(&b, r); err != nil {
		t.Fatal(err)
	}
	rows, err = csv.NewReader(&b).ReadAll()
	if err != nil || len(rows) != 3 || rows[0][3] != "memory_used_gib" {
		t.Fatal(b.String(), err)
	}
}

func TestGPUHealthNoDataAndHealthyCountsAreDistinct(t *testing.T) {
	r := &metricsquery.Result{Health: &metricsquery.GPUHealth{TotalCards: 8, UnknownCards: 8}}
	text := captureTableOutput(t, func() { printGPUHealth(r) })
	if !strings.Contains(text, "0 张卡无错误计数") || !strings.Contains(text, "8 张健康指标无数据") {
		t.Fatal(text)
	}
	r.Health.HealthyCards, r.Health.UnknownCards = 8, 0
	text = captureTableOutput(t, func() { printGPUHealth(r) })
	if !strings.Contains(text, "8 张卡无错误计数") {
		t.Fatal(text)
	}
}
