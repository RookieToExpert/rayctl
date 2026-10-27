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
		SharedVCPool: true, Scope: "vc_shared_candidate_pool", NodeCount: 2,
		Quota: &platform.SSPQueueQuota{Resources: []platform.SSPResourceSummary{
			{ResourceType: "NODE", Unit: "node", Total: "209", Allocated: "172", Unallocated: "37"},
			{ResourceType: "CPU", Unit: "core", Total: "23408", Allocated: "23280", Unallocated: "128"},
			{ResourceType: "MEMORY", Unit: "GiB", Total: "300960", Allocated: "180098", Unallocated: "120862"},
			{ResourceType: "DEVICE", Unit: "device", Total: "1672", Allocated: "1238", Unallocated: "434"},
		}, MachineTypes: []platform.SSPQueueMachineTypeQuota{{MachineType: "x2ls.ri.i80"}}},
		Items: []service.SSPQueueNodeUsageItem{
			{HostName: "host-free", CPUAllocated: "8", CPUTotal: "16", MemoryAllocated: "16GiB", MemoryTotal: "32GiB", AcceleratorAllocated: "7", AcceleratorTotalText: "8", AcceleratorFree: 1, AcceleratorTotal: 8},
			{HostName: "host-full", CPUAllocated: "16", CPUTotal: "16", MemoryAllocated: "16GiB", MemoryTotal: "32GiB", AcceleratorAllocated: "7", AcceleratorTotalText: "8", AcceleratorFree: 1, AcceleratorTotal: 8},
		},
	}
}

func TestQueueUsageShowsPageQuotaBeforeSharedPool(t *testing.T) {
	for _, columns := range []string{"80", "200"} {
		t.Run(columns, func(t *testing.T) {
			t.Setenv("COLUMNS", columns)
			result := queueQuotaOutputFixture()
			text := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result}) })
			split := strings.Index(text, "VC 共享候选池")
			if split < 0 || strings.Index(text, "队列配额") > split {
				t.Fatal(text)
			}
			for _, value := range []string{"209", "172", "37", "23408", "23280", "128", "300960", "180098", "120862", "1672", "1238", "434", "x2ls.ri.i80"} {
				if !strings.Contains(text[:split], value) {
					t.Fatalf("missing page quota %q:\n%s", value, text)
				}
			}
			if strings.Contains(text[:split], "host-free") || !strings.Contains(text[split:], "host-free") || !strings.Contains(text[split:], "host-full") || !strings.Contains(text, "不等于队列剩余配额") {
				t.Fatal(text)
			}
		})
	}
}

func TestQueueUsageFreeKeepsIdenticalQuotaAndExplainsFilter(t *testing.T) {
	result := queueQuotaOutputFixture()
	before := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result}) })
	result.FilterFreeNodes()
	after := captureTableOutput(t, func() { PrintSSPQueueNodeUsage([]*service.SSPQueueNodeUsageResult{result}) })
	if strings.Split(before, "VC 共享候选池")[0] != strings.Split(after, "VC 共享候选池")[0] {
		t.Fatalf("quota changed:\n%s", after)
	}
	for _, expected := range []string{"--free", "不校验队列配额", "不保证可调度", "配额表不受筛选影响", "共 2 台，本次显示 1 台", "host-free"} {
		if !strings.Contains(after, expected) {
			t.Fatalf("missing %q:\n%s", expected, after)
		}
	}
	if strings.Contains(after, "host-full") {
		t.Fatal(after)
	}
}

func TestQueueUsageJSONKeepsQuotaAndScopeWhenFree(t *testing.T) {
	result := queueQuotaOutputFixture()
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
	meta := payload.Metadata[0]
	if meta.Scope != "vc_shared_candidate_pool" || meta.NodeCount != 2 || !meta.FreeOnly || meta.Quota.Resources[1].Unallocated != "128" {
		t.Fatal(buffer.String())
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
				if !strings.Contains(text, "节点用量未能读取") || strings.Contains(text, "本次显示 0 台") {
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
	if strings.Count(text, "VC 共享候选池") != 2 {
		t.Fatal(text)
	}
}
