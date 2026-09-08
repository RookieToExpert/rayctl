package metrics

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeQueryClient struct{}

type knownPodClient struct {
	fakeQueryClient
	queries []string
}

func (c *knownPodClient) Export(ctx context.Context, match string, start, end time.Time) ([]RawSeries, error) {
	c.queries = append(c.queries, match)
	return c.fakeQueryClient.Export(ctx, match, start, end)
}

func TestKnownPodRestartSkipsMemoryDiscovery(t *testing.T) {
	client := &knownPodClient{}
	_, err := NewService(client).RestartForPods(context.Background(), QueryOptions{Workload: "demo", Type: "aid", Workspace: "ws", Pods: []string{"demo-0"}, Start: time.Unix(0, 0), End: time.Unix(100, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.queries) != 2 {
		t.Fatal(client.queries)
	}
	for _, query := range client.queries {
		if strings.Contains(query, memoryMetric) {
			t.Fatal("unnecessary memory discovery", query)
		}
	}
}

func (fakeQueryClient) Export(_ context.Context, match string, _ time.Time, _ time.Time) ([]RawSeries, error) {
	switch {
	case strings.Contains(match, workloadNameLabel):
		return []RawSeries{{Metric: map[string]string{
			"pod": "demo-0", "container": "main", "sco_srm_type": workspaceResourceType,
			workspaceNameLabel: "ws-a", workloadTypeLabel: "aid",
		}, Values: []float64{1}, Timestamps: []int64{1000}}}, nil
	case strings.HasPrefix(match, memoryLimitMetric):
		return []RawSeries{
			{Metric: map[string]string{"pod": "demo-0", "container": "main", "sco_srm_type": workspaceResourceType}, Values: []float64{20}, Timestamps: []int64{1000}},
			{Metric: map[string]string{"pod": "demo-0", "container": "sidecar", "sco_srm_type": workspaceResourceType}, Values: []float64{5}, Timestamps: []int64{1000}},
		}, nil
	default:
		return []RawSeries{
			{Metric: map[string]string{"pod": "demo-0", "container": "main", "namespace": "workspace", "sco_srm_type": workspaceResourceType}, Values: []float64{8, 8}, Timestamps: []int64{1000, 21000}},
			{Metric: map[string]string{"pod": "demo-0", "container": "main", "namespace": "vcluster", "sco_srm_type": workspaceResourceType}, Values: []float64{9, 7}, Timestamps: []int64{1000, 21000}},
			{Metric: map[string]string{"pod": "demo-0", "container": "main", "namespace": "legacy", "sco_srm_type": "compute.ecp.v1.virtualCluster"}, Values: []float64{50, 50}, Timestamps: []int64{1000, 21000}},
			{Metric: map[string]string{"pod": "demo-0", "container": "sidecar", "sco_srm_type": workspaceResourceType}, Values: []float64{2, 3}, Timestamps: []int64{1000, 21000}},
		}, nil
	}
}

func (fakeQueryClient) QueryRange(context.Context, string, time.Time, time.Time, time.Duration) ([]RawSeries, error) {
	return nil, nil
}

func (fakeQueryClient) Query(context.Context, string, time.Time) ([]RawSeries, error) {
	return nil, nil
}

func TestMemoryPrefersWorkspaceSourceAndMergesDuplicateSeries(t *testing.T) {
	result, err := NewService(fakeQueryClient{}).Memory(context.Background(), QueryOptions{
		Workload: "demo", Type: "auto", By: "pod", Start: time.UnixMilli(0), End: time.UnixMilli(3000),
	})
	if err != nil {
		t.Fatalf("Memory() error = %v", err)
	}
	if len(result.Samples) != 2 {
		t.Fatalf("sample count = %d, want 2", len(result.Samples))
	}
	if result.Samples[0].Value != 11 || result.Samples[1].Value != 11 {
		t.Fatalf("values = %#v, want 11 and 11", result.Samples)
	}
	if len(result.Series) != 1 || result.Series[0].Limit != 25 {
		t.Fatalf("series = %#v, want aggregate limit 25", result.Series)
	}
	if result.Series[0].MergedFrom != 4 || result.Series[0].DroppedSeries != 1 {
		t.Fatalf("merge metadata = %#v", result.Series[0])
	}
}

func TestRestartDetectsIncrementAndCounterReset(t *testing.T) {
	options := QueryOptions{Workload: "demo", By: "pod", Start: time.UnixMilli(2000), End: time.UnixMilli(6000)}
	identity := workloadIdentity{Type: "aid", Workspace: "ws", Pods: []string{"demo-0"}}
	restarts := []RawSeries{
		{Metric: map[string]string{"pod": "demo-0", "container": "main", "uid": "old"}, Values: []float64{1, 2}, Timestamps: []int64{1000, 3000}},
		{Metric: map[string]string{"pod": "demo-0", "container": "main", "uid": "new"}, Values: []float64{0, 1}, Timestamps: []int64{4000, 5000}},
	}
	reasons := []RawSeries{{Metric: map[string]string{"pod": "demo-0", "container": "main", "reason": "OOMKilled"}, Values: []float64{1}, Timestamps: []int64{5000}}}

	result := buildRestartResult(options, identity, restarts, reasons)
	if result.Summary.EventCount != 2 {
		t.Fatalf("event count = %d, want 2", result.Summary.EventCount)
	}
	if len(result.Samples) != 4 {
		t.Fatalf("samples = %#v, want baseline + 2 restarts + series reset", result.Samples)
	}
	events := map[string]int{}
	for _, sample := range result.Samples {
		events[sample.Event]++
	}
	if events["baseline"] != 1 || events["series_reset"] != 1 || events["restart"] != 2 {
		t.Fatalf("events = %#v", events)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected UID rebuild warning")
	}
	if got := result.Samples[len(result.Samples)-1].Reason; got != "OOMKilled" {
		t.Fatalf("reason = %q, want OOMKilled", got)
	}
}

func TestSelectorSupportsRegexAndNotEqual(t *testing.T) {
	got := selector("metric", map[string]string{"pod=~": "p.*", "container!=": "POD"})
	if !strings.Contains(got, `pod=~"p.*"`) || !strings.Contains(got, `container!="POD"`) {
		t.Fatalf("selector = %q", got)
	}
}

func TestMetricWorkloadTypeAliases(t *testing.T) {
	key, value := metricWorkloadTypeMatcher("ait")
	if key != workloadTypeLabel || value != "training-job" {
		t.Fatalf("ait matcher = %q %q", key, value)
	}
	key, value = metricWorkloadTypeMatcher("air")
	if key != workloadTypeLabel+"=~" || value != "^(?:air|infer-gateway)$" {
		t.Fatalf("air matcher = %q %q", key, value)
	}
	if got := normalizeMetricWorkloadType("training-job"); got != "ait" {
		t.Fatalf("normalized type = %q, want ait", got)
	}
	if got := normalizeMetricWorkloadType("infer-gateway"); got != "air" {
		t.Fatalf("normalized type = %q, want air", got)
	}
}

func TestMergeDuplicateRawSeriesUsesTimestampMax(t *testing.T) {
	labels := map[string]string{"pod": "demo-0", "container": "main", "uid": "uid-1"}
	merged := mergeDuplicateRawSeries([]RawSeries{
		{Metric: labels, Values: []float64{1, 2}, Timestamps: []int64{1000, 2000}},
		{Metric: map[string]string{"pod": "demo-0", "container": "main", "uid": "uid-1", "namespace": "vcluster"}, Values: []float64{3, 1}, Timestamps: []int64{1000, 2000}},
	})
	if len(merged) != 1 {
		t.Fatalf("series count = %d, want 1", len(merged))
	}
	if merged[0].Values[0] != 3 || merged[0].Values[1] != 2 {
		t.Fatalf("values = %#v, want [3 2]", merged[0].Values)
	}
}

func TestBuildResultPreservesAcceleratorPercent(t *testing.T) {
	result := buildResult("gpu", QueryOptions{
		Workload: "demo", Start: time.UnixMilli(0), End: time.UnixMilli(3000),
	}, workloadIdentity{Type: "aid"}, []*pointSeries{{
		name: "demo-0", unit: "percent", points: map[int64]float64{1000: 1, 2000: 75}, mergedFrom: 1,
	}})
	if result.Samples[0].Value != 1 || result.Samples[1].Value != 75 {
		t.Fatalf("values = %#v, want raw percentages 1 and 75", result.Samples)
	}
	if result.Series[0].Summary.Peak != 75 || result.Metadata.Unit != "percent" {
		t.Fatalf("result metadata/summary = %#v / %#v", result.Metadata, result.Series[0].Summary)
	}
}

func TestAggregateSeriesCoalescesNearbyScrapeTimestamps(t *testing.T) {
	input := []*pointSeries{
		{name: "pod/main/gpu=0", labels: map[string]string{"pod": "pod"}, unit: "percent", points: map[int64]float64{1000: 20, 31000: 60}},
		{name: "pod/main/gpu=1", labels: map[string]string{"pod": "pod"}, unit: "percent", points: map[int64]float64{2500: 80, 32500: 30}},
	}
	result := aggregateSeries(input, "pod", aggregateMax)
	if len(result) != 1 || len(result[0].points) != 2 {
		t.Fatalf("aggregated series = %#v", result)
	}
	if result[0].points[1000] != 80 || result[0].points[31000] != 60 {
		t.Fatalf("points = %#v, want max values 80 and 60", result[0].points)
	}
}
