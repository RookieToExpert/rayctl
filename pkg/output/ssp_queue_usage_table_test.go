package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"rayctl/internal/platform"
	"rayctl/internal/service"
)

func queueQuotaOutputFixture() *service.SSPQueueNodeUsageResult {
	return &service.SSPQueueNodeUsageResult{
		Queue:        service.SSPQueueItem{Name: "queue-elastic", Type: "ELASTIC", VCluster: "vc-shared"},
		SharedVCPool: true, Scope: "queue_owned_nodes", NodeCount: 2,
		Ownership: &service.SSPQueueOwnershipSummary{OwnedNodes: 2, EligibleNodes: 2},
		Quota: &platform.SSPQueueQuota{Resources: []platform.SSPResourceSummary{
			{ResourceType: "NODE", Unit: "node", Total: "209", Allocated: "172", Unallocated: "37"},
			{ResourceType: "CPU", Unit: "core", Total: "23408", Allocated: "23280", Unallocated: "128"},
			{ResourceType: "MEMORY", Unit: "GiB", Total: "300960", Allocated: "180098", Unallocated: "120862"},
			{ResourceType: "DEVICE", Unit: "device", Total: "1672", Allocated: "1238", Unallocated: "434"},
		}, MachineTypes: []platform.SSPQueueMachineTypeQuota{{MachineType: "x2ls.ri.i80"}}},
		Items: []service.SSPQueueNodeUsageItem{
			{HostName: "host-free", Ownership: "owned", CPUAllocated: "8", CPUTotal: "16", MemoryAllocated: "16GiB", MemoryTotal: "32GiB", AcceleratorAllocated: "7", AcceleratorTotalText: "8", AcceleratorFree: 1, AcceleratorTotal: 8},
			{HostName: "host-full", Ownership: "owned", CPUAllocated: "16", CPUTotal: "16", MemoryAllocated: "16GiB", MemoryTotal: "32GiB", AcceleratorAllocated: "7", AcceleratorTotalText: "8", AcceleratorFree: 1, AcceleratorTotal: 8},
		},
	}
}

func TestQueueUsageShowsPageQuotaBeforeOwnedNodesWithoutExplanations(t *testing.T) {
	for _, columns := range []string{"80", "200"} {
		t.Run(columns, func(t *testing.T) {
			t.Setenv("COLUMNS", columns)
			result := queueQuotaOutputFixture()
			text := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result}) })
			split := strings.Index(text, "本队列占用节点")
			if split < 0 || strings.Index(text, "队列配额") > split {
				t.Fatal(text)
			}
			for _, value := range []string{"209", "172", "37", "23408", "23280", "128", "300960", "180098", "120862", "1672", "1238", "434", "x2ls.ri.i80"} {
				if !strings.Contains(text[:split], value) {
					t.Fatalf("missing page quota %q:\n%s", value, text)
				}
			}
			if strings.Contains(text[:split], "host-free") || !strings.Contains(text[split:], "host-free") || !strings.Contains(text[split:], "host-full") || !strings.Contains(text, "共 2 台。") {
				t.Fatal(text)
			}
			assertNoQueueUsageExplanations(t, text)
		})
	}
}

func TestQueueUsageFreeKeepsIdenticalQuotaAndCompactOwnershipCounts(t *testing.T) {
	result := queueQuotaOutputFixture()
	other := result.Items[0]
	other.HostName, other.Ownership = "host-other", "other"
	result.Items = append(result.Items, other)
	public := result.Items[0]
	public.HostName, public.Ownership = "host-public", "unassigned"
	public.CPUAllocated, public.MemoryAllocated, public.AcceleratorAllocated = "0", "0GiB", "0"
	public.AcceleratorFree = 8
	result.Items = append(result.Items, public)
	result.NodeCount, result.Ownership.OtherNodes, result.Ownership.UnassignedNodes = 4, 1, 1
	before := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result}) })
	result.FilterFreeNodes()
	after := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result}) })
	if strings.SplitN(before, "\n\n", 2)[0] != strings.SplitN(after, "\n\n", 2)[0] {
		t.Fatalf("quota changed:\n%s", after)
	}
	for _, expected := range []string{"共 2 台：本队列 1，公共池未归属 1。", "host-free", "host-public", "本队列候选节点", "OWNERSHIP"} {
		if !strings.Contains(after, expected) {
			t.Fatalf("missing %q:\n%s", expected, after)
		}
	}
	if strings.Contains(after, "host-full") || strings.Contains(after, "host-other") || strings.Contains(after, "共 4 台") {
		t.Fatal(after)
	}
	assertNoQueueUsageExplanations(t, after)
}

func assertNoQueueUsageExplanations(t *testing.T, text string) {
	t.Helper()
	for _, noise := range []string{"平台资源页口径", "ELASTIC 按需使用", "ALLOC/TOTAL =", "Kubernetes allocatable", "--free 再筛选", "仅保留本队列", "不保证可调度", "VC 共"} {
		if strings.Contains(text, noise) {
			t.Fatalf("unexpected explanation %q:\n%s", noise, text)
		}
	}
}

func TestQueueUsageJSONKeepsQuotaAndScopeWhenFree(t *testing.T) {
	result := queueQuotaOutputFixture()
	result.Items[0].OwnerQueueUIDs = []string{"queue-uid"}
	result.Items[0].OwnerQueueNames = []string{result.Queue.Name}
	result.FilterFreeNodes()
	session := BeginJSON()
	defer EndJSON()
	PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result})
	var buffer bytes.Buffer
	if err := session.Write(&buffer, true, 1, nil); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items    []map[string]any
		Metadata []struct {
			Quota     platform.SSPQueueQuota
			Scope     string
			NodeCount int  `json:"node_count"`
			FreeOnly  bool `json:"free_only"`
		}
		Summary struct{ Complete bool }
	}
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || len(payload.Metadata) != 1 || !payload.Summary.Complete {
		t.Fatal(buffer.String())
	}
	for _, key := range []string{"owner_queue_uids", "owner_queue_names"} {
		values, ok := payload.Items[0][key].([]any)
		if !ok || len(values) != 1 {
			t.Fatalf("missing ownership evidence %q: %s", key, buffer.String())
		}
	}
	meta := payload.Metadata[0]
	if meta.Scope != "queue_owned_or_unassigned" || meta.NodeCount != 2 || !meta.FreeOnly || meta.Quota.Resources[1].Unallocated != "128" {
		t.Fatal(buffer.String())
	}
}

func TestQueueUsageDefaultJSONDescribesOwnedScope(t *testing.T) {
	result := queueQuotaOutputFixture()
	session := BeginJSON()
	defer EndJSON()
	PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result})
	var buffer bytes.Buffer
	if err := session.Write(&buffer, true, 1, nil); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items    []map[string]any
		Metadata []struct {
			Scope     string
			FreeOnly  bool `json:"free_only"`
			Ownership service.SSPQueueOwnershipSummary
		}
		Summary struct{ Complete bool }
	}
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Metadata) != 1 || len(payload.Items) != 2 || !payload.Summary.Complete || payload.Metadata[0].Scope != "queue_owned_nodes" || payload.Metadata[0].FreeOnly || payload.Metadata[0].Ownership.EligibleNodes != 2 {
		t.Fatal(buffer.String())
	}
	for _, item := range payload.Items {
		if item["ownership"] != "owned" {
			t.Fatal(item)
		}
	}
}

func TestQueueUsagePartialFailureDoesNotLookEmptyOrComplete(t *testing.T) {
	for _, nodeFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "quota", true: "nodes"}[nodeFailure], func(t *testing.T) {
			result := queueQuotaOutputFixture()
			if nodeFailure {
				result.Items = nil
				result.NodeUsageError = "节点用量查询失败（HTTP 403）"
			} else {
				result.Quota = nil
				result.QuotaError = "队列配额查询失败（HTTP 403）"
			}
			text := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result}) })
			if nodeFailure {
				if !strings.Contains(text, "节点用量未能读取") || strings.Contains(text, "共 0 台") {
					t.Fatal(text)
				}
			} else if !strings.Contains(text, "队列配额未能读取") || !strings.Contains(text, "host-free") {
				t.Fatal(text)
			}
			session := BeginJSON()
			defer EndJSON()
			PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result})
			var buffer bytes.Buffer
			if err := session.Write(&buffer, true, 1, nil); err != nil {
				t.Fatal(err)
			}
			var payload struct{ Summary struct{ Complete bool } }
			if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil || payload.Summary.Complete {
				t.Fatal(buffer.String(), err)
			}
		})
	}
}

func TestQueueUsageSeparatesMultipleQueuesAndMachineTypes(t *testing.T) {
	first, second := queueQuotaOutputFixture(), queueQuotaOutputFixture()
	second.Queue.Name = "queue-other"
	second.Quota.MachineTypes = append(second.Quota.MachineTypes, platform.SSPQueueMachineTypeQuota{MachineType: "other.cpu", Resources: []platform.SSPResourceSummary{{ResourceType: "CPU", Total: "32", Allocated: "0", Unallocated: "32"}}})
	text := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{first, second}) })
	for _, expected := range []string{"队列配额 [queue-elastic]", "队列配额 [queue-other]", "other.cpu", "合计"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q:\n%s", expected, text)
		}
	}
	if strings.Count(text, "本队列占用节点") != 2 {
		t.Fatal(text)
	}
}

func TestQueueOwnershipFailureKeepsQuotaAndDoesNotPrintEmptyPool(t *testing.T) {
	r := queueQuotaOutputFixture()
	r.Ownership = nil
	r.OwnershipError = "节点归属查询失败"
	r.FilterFreeNodes()
	text := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{r}) })
	for _, value := range []string{"128", "434", "节点归属未能核实"} {
		if !strings.Contains(text, value) {
			t.Fatal(text)
		}
	}
	if strings.Contains(text, "共 0 台") || strings.Contains(text, "host-free") {
		t.Fatal(text)
	}
}

func TestQueueOwnershipUnknownIsVisibleInJSONCompleteness(t *testing.T) {
	r := queueQuotaOutputFixture()
	r.Ownership.UnknownNodes = 1
	r.Ownership.OwnedNodes = 1
	r.Ownership.EligibleNodes = 1
	r.Items[1].Ownership = "unknown"
	r.Warnings = []string{"1 个节点归属无法确认，已排除"}
	r.FilterFreeNodes()
	session := BeginJSON()
	defer EndJSON()
	PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{r})
	var buffer bytes.Buffer
	if err := session.Write(&buffer, true, 1, nil); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Summary  struct{ Complete bool }
		Warnings []string
		Metadata []struct {
			Ownership service.SSPQueueOwnershipSummary
		}
	}
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Summary.Complete || len(payload.Warnings) != 1 || payload.Metadata[0].Ownership.UnknownNodes != 1 {
		t.Fatal(buffer.String())
	}
}
