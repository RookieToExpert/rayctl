package metrics

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

type gpuFixtureClient struct {
	data    []RawSeries
	queries []string
}

func (c *gpuFixtureClient) Export(_ context.Context, query string, _ time.Time, _ time.Time) ([]RawSeries, error) {
	c.queries = append(c.queries, query)
	return c.selectSeries(query), nil
}
func (c *gpuFixtureClient) QueryRange(ctx context.Context, q string, a, b time.Time, _ time.Duration) ([]RawSeries, error) {
	return c.Export(ctx, q, a, b)
}
func (c *gpuFixtureClient) Query(context.Context, string, time.Time) ([]RawSeries, error) {
	return nil, nil
}
func (c *gpuFixtureClient) selectSeries(query string) []RawSeries {
	matchers := regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)(=~|!=|=)("(?:[^"\\]|\\.)*")`).FindAllStringSubmatch(query, -1)
	out := []RawSeries{}
	for _, item := range c.data {
		matches := true
		for _, m := range matchers {
			v, _ := strconv.Unquote(m[3])
			actual := item.Metric[m[1]]
			switch m[2] {
			case "=":
				matches = matches && actual == v
			case "!=":
				matches = matches && actual != v
			case "=~":
				ok, _ := regexp.MatchString(v, actual)
				matches = matches && ok
			}
		}
		if matches {
			out = append(out, item)
		}
	}
	return out
}

func muxiFixture() *gpuFixtureClient {
	c := &gpuFixtureClient{}
	for gpu := 0; gpu < 8; gpu++ {
		pod := fmt.Sprintf("demo-%d", gpu)
		c.data = append(c.data, RawSeries{Metric: map[string]string{"__name__": memoryMetric, "pod": pod, "container": "main", workloadNameLabel: "demo", workspaceNameLabel: "ws", workloadTypeLabel: "air", "sco_srm_type": workspaceResourceType}, Values: []float64{1}, Timestamps: []int64{1000}})
		for _, view := range []string{workspaceResourceType, "compute.ecp.v1.virtualCluster", "compute.ecp.v1.aiComputeNode"} {
			labels := map[string]string{"__name__": "mx_gpu_usage", "pod": "exporter", "exported_pod": pod, "gpu": strconv.Itoa(gpu), "Hostname": "host-1", "uuid": fmt.Sprintf("GPU-%d", gpu), "modelName": "MXC550-PL", "sco_srm_type": view}
			c.data = append(c.data, RawSeries{Metric: cloneLabels(labels), Values: []float64{0, 0}, Timestamps: []int64{1000, 31000}})

			wrapped := cloneLabels(labels)
			wrapped["__name__"], wrapped["extended_resource__type"] = "lepton__ssp__gpu_util", "mx.com/gpu"
			wrapped["pod"], wrapped["container"] = pod, "aispace"
			delete(wrapped, "exported_pod")
			c.data = append(c.data, RawSeries{Metric: wrapped, Values: []float64{0, 0}, Timestamps: []int64{1000, 31000}})
			labels["__name__"], labels["type"] = "mx_memory_used", "vram"
			used := 42.0
			if gpu == 1 {
				used = .8
			}
			c.data = append(c.data, RawSeries{Metric: cloneLabels(labels), Values: []float64{used * 1024 * 1024, used * 1024 * 1024}, Timestamps: []int64{1000, 31000}})
			labels["type"] = "xtt"
			c.data = append(c.data, RawSeries{Metric: cloneLabels(labels), Values: []float64{3386788}, Timestamps: []int64{1000}})
			labels["__name__"], labels["type"] = "mx_memory_total", "vram"
			delete(labels, "exported_pod")
			c.data = append(c.data, RawSeries{Metric: cloneLabels(labels), Values: []float64{64 * 1024 * 1024}, Timestamps: []int64{1000}})
			labels["__name__"], labels["exported_pod"] = "mx_ecc_error_count", pod
			ecc := 0.0
			if gpu == 1 {
				ecc = 2
			}
			c.data = append(c.data, RawSeries{Metric: cloneLabels(labels), Values: []float64{ecc}, Timestamps: []int64{1000}})
			if gpu == 1 {
				labels["__name__"] = "mx_ras_count"
				c.data = append(c.data, RawSeries{Metric: cloneLabels(labels), Values: []float64{1}, Timestamps: []int64{1000}})
			}
		}
	}
	return c
}

func TestGPURegressionMuxiMemoryHealthAndDedup(t *testing.T) {
	for _, step := range []bool{false, true} {
		t.Run(fmt.Sprint(step), func(t *testing.T) {
			client := muxiFixture()
			result, err := NewService(client, "d").GPU(context.Background(), QueryOptions{Workload: "demo", Workspace: "ws", By: "gpu", Health: true, UseStep: step, Step: 30 * time.Second, Start: time.Unix(0, 0), End: time.Unix(60, 0)})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Series) != 8 || len(result.Samples) != 16 {
				t.Fatalf("series=%d samples=%d", len(result.Series), len(result.Samples))
			}
			for _, series := range result.Series {
				want := 42.0
				if *series.GPU == "1" {
					want = .8
				}
				if series.MemoryUsedGiB == nil || math.Abs(*series.MemoryUsedGiB-want) > 1e-6 || series.MemoryTotalGiB == nil || *series.MemoryTotalGiB != 64 {
					t.Fatalf("wrong memory: %+v", series)
				}
			}
			h := result.Health
			if h.TotalCards != 8 || h.HealthyCards != 7 || h.UnknownCards != 0 || len(h.Anomalies) != 1 {
				t.Fatalf("health=%+v", h)
			}
			bad := h.Anomalies[0]
			if bad.GPU != "1" || bad.UUID != "GPU-1" || *bad.RAS != 1 || *bad.ECC != 2 {
				t.Fatalf("bad=%+v", bad)
			}
			for _, q := range client.queries {
				if strings.Contains(q, "mx_memory_") && !strings.Contains(q, `type="vram"`) {
					t.Fatal(q)
				}
				if strings.Contains(q, "mx_gpu_state") || strings.Contains(q, "mx_ras_status") {
					t.Fatal(q)
				}
			}
		})
	}
}

func TestGPUUnknownHealthDoesNotReportHealthy(t *testing.T) {
	c := muxiFixture()
	keep := []RawSeries{}
	for _, s := range c.data {
		if !strings.Contains(s.Metric["__name__"], "ecc") && !strings.Contains(s.Metric["__name__"], "ras") {
			keep = append(keep, s)
		}
	}
	c.data = keep
	r, err := NewService(c).GPU(context.Background(), QueryOptions{Workload: "demo", By: "pod", Health: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Health.HealthyCards != 0 || r.Health.UnknownCards != 8 {
		t.Fatalf("health=%+v", r.Health)
	}
}

func TestGPUVendorMemoryUnitsAndProductLines(t *testing.T) {
	for _, tc := range []struct {
		name, hardware, util, used, total string
		usedValue, totalValue             float64
	}{
		{"pt-ait", "nvidia.com/gpu", "lepton__ait__gpu_util", "lepton__ait__gpu_memory_used__MiB", "lepton__ait__gpu_memory_total__GiB", 4096, 80},
		{"pt-ssp", "nvidia.com/gpu", "lepton__ssp__gpu_util", "lepton__ssp__gpu_memory_used__MiB", "lepton__ssp__gpu_memory_total__bytes", 4096, 80 * 1024 * 1024 * 1024},
		{"npu", "npu.com/gpu", "npu_chip_info_utilization", "npu_chip_info_hbm_used_memory", "npu_chip_info_hbm_total_memory", 4096, 32768},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &gpuFixtureClient{}
			for metric, value := range map[string]float64{memoryMetric: 1, tc.util: 25, tc.used: tc.usedValue, tc.total: tc.totalValue} {
				c.data = append(c.data, RawSeries{Metric: map[string]string{"__name__": metric, "pod": "demo-0", "container": "main", "gpu": "0", "node": "host-1", workloadNameLabel: "demo", workspaceNameLabel: "ws", "sco_srm_type": workspaceResourceType}, Values: []float64{value}, Timestamps: []int64{1000}})
			}
			r, err := NewService(c).GPU(context.Background(), QueryOptions{Workload: "demo", By: "gpu"})
			if err != nil {
				t.Fatal(err)
			}
			s := r.Series[0]
			if s.Hardware != tc.hardware || s.MemoryUsedGiB == nil || *s.MemoryUsedGiB != 4 || s.MemoryPercent == nil {
				t.Fatalf("series=%+v", s)
			}
		})
	}
}

func TestGPUBaselineIncludesUnselectedReplicas(t *testing.T) {
	r, err := NewService(muxiFixture()).GPU(context.Background(), QueryOptions{Workload: "demo", By: "gpu", Pods: []string{"demo-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Series) != 1 || r.Baseline == nil || r.Baseline.PodCount != 8 || r.Baseline.MemoryP50GiB == nil || *r.Baseline.MemoryP50GiB != 42 {
		t.Fatalf("result=%+v baseline=%+v", r, r.Baseline)
	}
}

func TestGPUSummaryStatistics(t *testing.T) {
	s := SeriesSummary{}
	completeSeriesSummary(&s, map[int64]float64{1: 0, 2: 0, 3: 50, 4: 100})
	if s.Average != 37.5 || s.P95 != 92.49999999999999 && s.P95 != 92.5 || s.NonzeroPercent != 50 {
		t.Fatal(s)
	}
}

func TestNPUHealthDeduplicatesSingleDoubleAndIsolatedPages(t *testing.T) {
	labels := map[string]string{"pod": "p", "node": "n", "gpu": "0", "extended_resource__type": "npu.com/gpu"}
	cards := []RawSeries{{Metric: labels}}
	values := []RawSeries{}
	for name, value := range map[string]float64{"single": 2, "double": 3} {
		for i := 0; i < 3; i++ {
			l := cloneLabels(labels)
			l["__name__"] = name
			values = append(values, RawSeries{Metric: l, Values: []float64{value}, Timestamps: []int64{1000}})
		}
	}
	h := gpuHealth(cards, map[string][]RawSeries{"ecc": values, "isolated_pages": {{Metric: labels, Values: []float64{1}}}})
	if len(h.Anomalies) != 1 || *h.Anomalies[0].ECC != 5 || *h.Anomalies[0].IsolatedPages != 1 {
		t.Fatalf("health=%+v", h)
	}
}

func TestGPUBaselineFlagsLowMemoryWithoutTreatingZeroUtilAsFault(t *testing.T) {
	all := &Result{}
	normal, bad := 42.0, .8
	for i := 0; i < 100; i++ {
		m := normal
		if i == 0 {
			m = bad
		}
		all.Series = append(all.Series, Series{MemoryUsedGiB: &m})
	}
	selected := &Result{Series: []Series{{Pod: stringPointer("p"), MemoryUsedGiB: &bad}}}
	b := gpuBaseline(all, selected)
	if !b.SignificantlyLow || b.UtilMax != 0 || *b.SelectedMemoryPercentile != .5 {
		t.Fatalf("baseline=%+v", b)
	}
}
