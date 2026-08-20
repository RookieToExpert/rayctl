package output

import (
	"strings"
	"testing"

	"rayctl/internal/service"
)

func TestPrintSSPQueueListKeepsLongQueueOnOneLine(t *testing.T) {
	queueName := "queue-d-reserved-muxi-h3c-data-producer"
	result := &service.SSPQueueListResult{Items: []service.SSPQueueItem{{
		Name:      queueName,
		State:     "RUNNING",
		Type:      "EXCLUSIVE",
		Workspace: "ws-d-muxi-h3c-data-producer",
		VCluster:  "vc-muxi-c550-ailab",
		Region:    "cn-pj-01",
	}}}

	text := captureTableOutput(t, func() { PrintSSPQueueList(result) })
	if !strings.Contains(text, "queue-d-reserved-muxi-h3c") {
		t.Fatalf("queue name is missing from output:\n%s", text)
	}
	dataLines := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "│ ") && !strings.Contains(line, "QUEUE") {
			dataLines++
		}
	}
	if dataLines != 1 {
		t.Fatalf("queue row used %d lines, want one:\n%s", dataLines, text)
	}
}

func TestPrintSSPClusterDetailIncludesSummaryAndQueues(t *testing.T) {
	result := &service.SSPClusterItem{
		Name: "cluster-a3", UID: "cluster-uid", State: "RUNNING", VCluster: "vc-a3", QueueCount: 1, NodeCount: 144,
		Resources: []service.SSPClusterResourceItem{{ResourceType: "DEVICE", Allocated: "32", Total: "144", Unallocated: "112", Unit: "device"}},
		Queues:    []service.SSPQueueItem{{Name: "queue-demo", State: "RUNNING", Type: "EXCLUSIVE", Workspace: "ws-demo", NodeCount: 4}},
	}
	text := captureTableOutput(t, func() { PrintSSPClusterDetail(result) })
	for _, expected := range []string{"cluster-a3", "vc-a3", "32/144", "queue-demo", "ws-demo"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, text)
		}
	}
}

func TestPrintSSPQueueDetailIncludesPolicies(t *testing.T) {
	text := captureTableOutput(t, func() {
		PrintSSPQueueDetail(&service.SSPQueueItem{Name: "queue-demo", SpotLending: "开启", DequeuePolicy: "均衡"})
	})
	for _, expected := range []string{"空闲资源借出", "开启", "排队策略", "均衡"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, text)
		}
	}
}

func TestPrintSSPQueueWorkloads(t *testing.T) {
	results := []*service.SSPQueueWorkloadResult{{
		Queue: service.SSPQueueItem{Name: "queue-demo"},
		Items: []service.SSPQueueWorkloadItem{{Type: "trainingJob", Name: "job-demo", State: "Running", Workspace: "ws-demo", Priority: "NORMAL", Resources: "worker×1 32C", Creator: "tester", CreatedAt: "2026-08-19 14:32:02"}},
	}}
	text := captureTableOutput(t, func() { PrintSSPQueueWorkloads(results) })
	for _, expected := range []string{"JOB", "job-demo", "Running", "ws-demo", "tester"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "RESOURCES") || strings.Contains(text, "worker×1 32C") {
		t.Fatalf("resource details should not be shown in workload summary:\n%s", text)
	}
}
