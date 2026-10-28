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
	if !strings.Contains(text, "本次共 1 条。") {
		t.Fatalf("queue total is missing from output:\n%s", text)
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

func TestPrintSSPWorkspaceDetailShowsQueueRuntimeAndLongUID(t *testing.T) {
	result := &service.SSPWorkspaceItem{
		Name: "ws-demo", UID: "misleading-workspace-runtime", State: "RUNNING", VCluster: "misleading-vc",
		Queues: []service.SSPQueueItem{{
			Name: "queue-demo", UID: "queue-uid", State: "RUNNING", Type: "EXCLUSIVE", Cluster: "cluster-demo", VCluster: "vc-demo", Namespace: "namespace-demo",
		}},
	}

	shortText := captureTableOutput(t, func() { PrintSSPWorkspaceDetail(result, false) })
	for _, expected := range []string{"WORKSPACE", "ws-demo", "QUEUE", "CLUSTER", "cluster-demo", "VC", "NAMESPACE", "vc-demo", "namespace-demo"} {
		if !strings.Contains(shortText, expected) {
			t.Fatalf("short workspace output does not contain %q:\n%s", expected, shortText)
		}
	}
	for _, omitted := range []string{"misleading-workspace-runtime", "misleading-vc", "queue-uid"} {
		if strings.Contains(shortText, omitted) {
			t.Fatalf("short workspace output unexpectedly contains %q:\n%s", omitted, shortText)
		}
	}
	longText := captureTableOutput(t, func() { PrintSSPWorkspaceDetail(result, true) })
	if !strings.Contains(longText, "queue-uid") {
		t.Fatalf("long workspace output does not contain queue UID:\n%s", longText)
	}
	if strings.Contains(longText, "misleading-workspace-runtime") || strings.Contains(longText, "misleading-vc") {
		t.Fatalf("long workspace output contains misleading main runtime fields:\n%s", longText)
	}
}

func TestPrintAIRDetailsShowVirtualAndHostNamespaces(t *testing.T) {
	jobText := captureTableOutput(t, func() {
		PrintSSPAIRJobDetail(&service.SSPAIRJobItem{Cluster: "cluster-muxi", VCluster: "vc-muxi-c550-ailab", Namespace: "virtual-ns", HostNamespace: "host-ns"}, false)
	})
	for _, expected := range []string{"CLUSTER", "cluster-muxi", "VC", "vc-muxi-c550-ailab"} {
		if !strings.Contains(jobText, expected) {
			t.Fatalf("AIR job output missing %q:\n%s", expected, jobText)
		}
	}
	gatewayText := captureTableOutput(t, func() {
		PrintSSPAIRGatewayDetail(&service.SSPAIRGatewayItem{Namespace: "virtual-ns", HostNamespace: "host-ns"}, false)
	})
	for _, text := range []string{jobText, gatewayText} {
		for _, expected := range []string{"VC NAMESPACE", "virtual-ns", "HOST NAMESPACE", "host-ns"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("missing %q:\n%s", expected, text)
			}
		}
	}
}

func TestWorkloadDetailsShowVirtualAndHostNamespaces(t *testing.T) {
	outputs := []string{
		captureTableOutput(t, func() {
			PrintJobDetail(&service.JobGetResult{Namespace: "virtual-ns", HostNamespace: "host-ns", Terminal: true}, false, false)
		}),
		captureTableOutput(t, func() {
			PrintSSPJobDetail(&service.SSPJobGetResult{Namespace: "virtual-ns", HostNamespace: "host-ns", Terminal: true}, false)
		}),
		captureTableOutput(t, func() {
			PrintSSPAIDDetail(&service.SSPAIDGetResult{Namespace: "virtual-ns", HostNamespace: "host-ns", Terminal: true}, false, false)
		}),
	}
	for _, text := range outputs {
		for _, expected := range []string{"VC NAMESPACE", "virtual-ns", "HOST NAMESPACE", "host-ns"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("missing %q:\n%s", expected, text)
			}
		}
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
		Node: "host-a", Creator: "test-user", Resource: "8C/32GiB", CreatedAt: "2026-09-02 12:00:00",
	}}}
	shortText := captureTableOutput(t, func() { PrintSSPAIDList(result, false) })
	if strings.Contains(shortText, "NODE") || strings.Contains(shortText, "RESOURCE") || strings.Contains(shortText, "CREATED") || strings.Contains(shortText, "8C/32GiB") {
		t.Fatalf("short AID list contains long columns:\n%s", shortText)
	}
	longText := captureTableOutput(t, func() { PrintSSPAIDList(result, true) })
	for _, expected := range []string{"NODE", "host-a", "RESOURCE", "CREATED", "8C/32GiB", "2026-09-02 12:00:00"} {
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

func TestPrintSSPQueueDetailLongIncludesRuntimeLimits(t *testing.T) {
	text := captureTableOutput(t, func() {
		PrintSSPQueueDetail(&service.SSPQueueItem{
			Name: "queue-demo", MachineTypes: "acn.g1k.60xlarge", MaxNodes: "acn.g1k.60xlarge=1", Capability: "CPU=240, MEMORY=960Gi",
		}, true)
	})
	for _, expected := range []string{"MACHINE TYPES", "acn.g1k.60xlarge", "MAX NODES", "CAPABILITY", "CPU=240, MEMORY=960Gi"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("long queue output does not contain %q:\n%s", expected, text)
		}
	}
}

func TestPrintSSPQueueNodeListLongOnlyShowsNewUID(t *testing.T) {
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
	for _, expected := range []string{"HOST", "ACN UID", "acn-uid"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q:\n%s", expected, text)
		}
	}
	for _, omitted := range []string{"MODEL", "MXC550-PL", "MACHINE TYPE", "STATE", "QUEUE UID"} {
		if strings.Contains(text, omitted) {
			t.Fatalf("default field %q repeated in incremental view:\n%s", omitted, text)
		}
	}
}

func TestPrintElasticQueueNodeListAndUsageHaveDistinctScopes(t *testing.T) {
	queue := service.SSPQueueItem{Name: "queue-elastic", Type: "ELASTIC", VCluster: "vc-cpu"}
	listText := captureTableOutput(t, func() {
		PrintSSPQueueNodeList(&service.SSPQueueNodeListResult{Queue: queue, SharedVCPool: true}, false)
	})
	usageText := captureTableOutput(t, func() {
		PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{{Queue: queue, SharedVCPool: true}})
	})
	for _, expected := range []string{"ELASTIC", "共享候选池", "优先复用", "非队列独占"} {
		if !strings.Contains(listText, expected) {
			t.Fatalf("missing %q:\n%s", expected, listText)
		}
	}
	if !strings.Contains(usageText, "本队列占用节点") || strings.Contains(usageText, "共享候选池") {
		t.Fatal(usageText)
	}
	assertNoQueueUsageExplanations(t, usageText)
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

func TestPrintWorkspaceNodeUsageIncludesQueuesAndSharedPoolNotice(t *testing.T) {
	result := &service.SSPWorkspaceNodeUsageResult{
		Workspace:     service.SSPWorkspaceItem{Name: "ws-demo"},
		SharedVCPools: true,
		Items: []service.SSPQueueNodeUsageItem{{
			Queues: "queue-a, queue-b", HostName: "host-10-0-0-1", HostIP: "10.0.0.1",
			CPUAllocated: "8", CPUTotal: "16", MemoryAllocated: "16GiB", MemoryTotal: "32GiB",
		}},
	}
	text := captureTableOutput(t, func() { PrintSSPWorkspaceNodeUsage([]*service.SSPWorkspaceNodeUsageResult{result}) })
	for _, expected := range []string{"QUEUES", "queue-a, queue-b", "host-10-0-0-1", "8/16", "共享候选池", "相同节点"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q:\n%s", expected, text)
		}
	}
}

func TestCompactWorkspaceQueueNames(t *testing.T) {
	got := compactWorkspaceQueueNames("ws-d-a3-ai4s", "queue-d-elastic-cpu-ai4s, queue-d-reserved-a3-ai4s")
	if got != "elastic-cpu-ai4s, reserved-a3-ai4s" {
		t.Fatalf("compact queue names = %q", got)
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
