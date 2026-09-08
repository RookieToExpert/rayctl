package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"reflect"
	"testing"

	metricsquery "rayctl/internal/metrics"
)

func TestMetricsJSONContractIncludesNullStepAndSeriesSummary(t *testing.T) {
	result := metricsquery.Result{
		Metadata: metricsquery.Metadata{
			Command: "mem", Workload: "demo", Metric: "container_memory_working_set_bytes", Unit: "bytes",
			Mode: "raw_samples", Range: metricsquery.TimeRange{Start: "2026-09-04T10:00:00+08:00", End: "2026-09-04T11:00:00+08:00"},
			SampleIntervalSeconds: 20,
		},
		Series: []metricsquery.Series{{
			Name: "demo-0/main", Pod: stringTestPointer("demo-0"), Container: stringTestPointer("main"), Unit: "bytes", Limit: 1024,
			MergedFrom: 3, Summary: metricsquery.SeriesSummary{Peak: 900, Latest: 100, SampleCount: 2},
		}},
		Samples:  []metricsquery.Sample{{Series: "demo-0/main", Timestamp: "2026-09-04T10:00:20+08:00", Value: 512, Percent: 50}},
		Summary:  metricsquery.Summary{SeriesCount: 1, SampleCount: 2},
		Warnings: []string{},
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	metadata := decoded["metadata"].(map[string]any)
	if step, exists := metadata["step"]; !exists || step != nil {
		t.Fatalf("metadata.step = %#v, exists=%v; want explicit null", step, exists)
	}
	series := decoded["series"].([]any)[0].(map[string]any)
	if series["gpu"] != nil || series["summary"] == nil {
		t.Fatalf("series contract = %#v", series)
	}
	summary := decoded["summary"].(map[string]any)
	if _, exists := summary["series"]; exists {
		t.Fatalf("top-level summary must not contain per-series data: %#v", summary)
	}
	sample := decoded["samples"].([]any)[0].(map[string]any)
	if _, exists := sample["percent"]; exists {
		t.Fatalf("JSON samples must retain raw values only: %#v", sample)
	}
}

func TestRestartCSVIncludesEventColumns(t *testing.T) {
	result := &metricsquery.Result{
		Metadata: metricsquery.Metadata{Command: "restart"},
		Samples: []metricsquery.Sample{{
			Timestamp: "2026-09-04T11:09:27+08:00", Series: "demo-0/main", Value: 2,
			Delta: 1, Reason: "OOMKilled", UID: "uid-1", Event: "restart",
		}},
	}
	var buffer bytes.Buffer
	if err := writeMetricsCSV(&buffer, result); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&buffer).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	wantHeader := []string{"time", "series", "restarts", "total", "reason", "uid", "event"}
	if !reflect.DeepEqual(records[0], wantHeader) {
		t.Fatalf("header = %#v, want %#v", records[0], wantHeader)
	}
	wantRow := []string{"2026-09-04T11:09:27+08:00", "demo-0/main", "1", "2", "OOMKilled", "uid-1", "restart"}
	if !reflect.DeepEqual(records[1], wantRow) {
		t.Fatalf("restart row = %#v, want %#v", records[1], wantRow)
	}
}

func TestMetricsRawJSONPreservesPrometheusLabels(t *testing.T) {
	result := metricsquery.Result{
		Metadata: metricsquery.Metadata{Command: "raw", Unit: "unknown"},
		Series: []metricsquery.Series{{
			Name: "series-1", Unit: "unknown", Labels: map[string]string{"workload_type": "training-job"},
			DroppedSCOSRMTypes: []string{},
		}},
		Samples: []metricsquery.Sample{}, Warnings: []string{},
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"labels":{"workload_type":"training-job"}`)) {
		t.Fatalf("raw labels missing from JSON: %s", payload)
	}
}

func stringTestPointer(value string) *string {
	return &value
}
