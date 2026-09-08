package nodehealth

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"

	metricsquery "rayctl/internal/metrics"
)

type fakeMetricsQuerier struct {
	mu      sync.Mutex
	queries []string
}

func (f *fakeMetricsQuerier) Query(_ context.Context, query string, _ time.Time) ([]metricsquery.RawSeries, error) {
	f.mu.Lock()
	f.queries = append(f.queries, query)
	f.mu.Unlock()
	value := 20.0
	if strings.Contains(query, "files_free") {
		value = 3
	}
	return []metricsquery.RawSeries{{
		Metric: map[string]string{"mountpoint": "/", "device": "/dev/root", "fstype": "ext4"},
		Values: []float64{value},
	}}, nil
}

func TestNoExecSkipsOnlyExecChecks(t *testing.T) {
	node := healthyNode("host-1")
	client := fake.NewSimpleClientset(node)
	metrics := &fakeMetricsQuerier{}
	service := NewService(client, metrics, nil, "pt")
	result, err := service.Run(t.Context(), []*corev1.Node{node}, Options{
		Checks: []string{CheckNodeBasic, CheckDisk, CheckDevicePlugin, CheckClockSkew, CheckKernelErrors},
		NoExec: true,
		Now:    func() time.Time { return time.Unix(100, 0) },
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	metrics.mu.Lock()
	queryCount := len(metrics.queries)
	metrics.mu.Unlock()
	if queryCount != 2 {
		t.Fatalf("disk query count = %d, want 2", queryCount)
	}
	if result.Nodes[0].Status != StatusOK {
		t.Fatalf("node status = %s, want OK", result.Nodes[0].Status)
	}
	for _, check := range result.Nodes[0].Checks[2:] {
		if check.Status != StatusSkip || check.Evidence["reason"] != "--no-exec" {
			t.Fatalf("exec check was not skipped: %#v", check)
		}
	}
}

func TestResolveNodesSupportsIP(t *testing.T) {
	node := healthyNode("custom-host")
	node.Status.Addresses = append(node.Status.Addresses, corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.1.2.3"})
	service := NewService(fake.NewSimpleClientset(node), nil, nil, "d")
	result, err := service.ResolveNodes(t.Context(), []string{"10.1.2.3"})
	if err != nil || len(result) != 1 || result[0].Name != "custom-host" {
		t.Fatalf("ResolveNodes() = %#v, %v", result, err)
	}
}

func TestNodeBasicCordonFails(t *testing.T) {
	node := healthyNode("host-1")
	node.Spec.Unschedulable = true
	node.Spec.Taints = []corev1.Taint{{Key: "maintenance", Effect: corev1.TaintEffectNoSchedule}}
	result := checkNodeBasic(node)
	if result.Status != StatusFail {
		t.Fatalf("checkNodeBasic() = %#v, want FAIL", result)
	}
}
