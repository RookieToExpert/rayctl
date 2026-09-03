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

func TestPrintSSPCatalogListColumns(t *testing.T) {
	result := &service.SSPCatalogListResult{Items: []service.SSPCatalogListItem{{
		Name: "job-demo", State: "Running", Workspace: "ws-demo", Queue: "queue-demo", Creator: "test-user", CreatedAt: "2026-09-02 12:00:00",
	}}}
	text := captureTableOutput(t, func() { PrintSSPAITList(result) })
	for _, expected := range []string{"NAME", "STATE", "WORKSPACE", "QUEUE", "CREATOR", "CREATED", "job-demo", "queue-demo", "本次筛选共 1 条。"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("catalog output does not contain %q:\n%s", expected, text)
		}
	}
}

func TestPrintSSPAIDListOnlyShowsResourceAndCreatedInLongMode(t *testing.T) {
	result := &service.SSPCatalogListResult{Items: []service.SSPCatalogListItem{{
		Name: "dev-demo", State: "Running", Workspace: "ws-demo", Queue: "queue-demo",
		Creator: "test-user", Resource: "8C/32GiB", CreatedAt: "2026-09-02 12:00:00",
	}}}
	shortText := captureTableOutput(t, func() { PrintSSPAIDList(result, false) })
	if strings.Contains(shortText, "RESOURCE") || strings.Contains(shortText, "CREATED") || strings.Contains(shortText, "8C/32GiB") {
		t.Fatalf("short AID list contains long columns:\n%s", shortText)
	}
	longText := captureTableOutput(t, func() { PrintSSPAIDList(result, true) })
	for _, expected := range []string{"RESOURCE", "CREATED", "8C/32GiB", "2026-09-02 12:00:00"} {
		if !strings.Contains(longText, expected) {
			t.Fatalf("long AID list does not contain %q:\n%s", expected, longText)
		}
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
		PrintSSPQueueDetail(&service.SSPQueueItem{Name: "queue-demo", SpotLending: "开启", DequeuePolicy: "均衡"}, false)
	})
	for _, expected := range []string{"空闲资源借出", "开启", "排队策略", "均衡"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, text)
		}
	}
}

func TestPrintSSPQueueNodeListLongOmitsDuplicateACNName(t *testing.T) {
	result := &service.SSPQueueNodeListResult{
		Queue: service.SSPQueueItem{Name: "queue-demo", UID: "queue-uid"},
		Items: []service.VCNodeListItem{{
			HostName: "host-10-0-0-1", Name: "host-10-0-0-1", UID: "acn-uid", Model: "MXC550-PL",
		}},
	}

	text := captureTableOutput(t, func() { PrintSSPQueueNodeList(result, true) })
	if strings.Count(text, "ACN") != 1 {
		t.Fatalf("unexpected duplicate ACN column:\n%s", text)
	}
	for _, expected := range []string{"HOST", "MODEL", "ACN UID", "MXC550-PL", "acn-uid"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q:\n%s", expected, text)
		}
	}
}

func TestPrintElasticQueueNodeViewsExplainSharedVCPool(t *testing.T) {
	queue := service.SSPQueueItem{Name: "queue-elastic", Type: "ELASTIC", VCluster: "vc-cpu"}
	listText := captureTableOutput(t, func() {
		PrintSSPQueueNodeList(&service.SSPQueueNodeListResult{Queue: queue, SharedVCPool: true}, false)
	})
	usageText := captureTableOutput(t, func() {
		PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{{Queue: queue, SharedVCPool: true}})
	})
	for _, output := range []string{listText, usageText} {
		for _, expected := range []string{"ELASTIC", "共享候选池", "优先复用", "非队列独占"} {
			if !strings.Contains(output, expected) {
				t.Fatalf("missing %q:\n%s", expected, output)
			}
		}
	}
}

func TestPrintQueueNodeUsageHidesAcceleratorForCPUOnlyNodes(t *testing.T) {
	result := &service.SSPQueueNodeUsageResult{Items: []service.SSPQueueNodeUsageItem{{
		HostName: "cpu-node", CPUTotal: "255.9", MemoryTotal: "1005GiB",
	}}}

	text := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result}) })

	if strings.Contains(text, "ACCEL") {
		t.Fatalf("CPU-only output contains ACCEL column:\n%s", text)
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
