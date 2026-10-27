package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"rayctl/internal/platform"
)

func TestQueueNodeUsageRetainsQuotaAndPartialResults(t *testing.T) {
	for _, test := range []struct {
		name                     string
		quotaDenied, nodesDenied bool
	}{
		{name: "complete"}, {name: "quota unavailable", quotaDenied: true}, {name: "nodes unavailable", nodesDenied: true}, {name: "both unavailable", quotaDenied: true, nodesDenied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/queues/queue-test/nodes"):
					if test.nodesDenied {
						w.WriteHeader(403)
						return
					}
					fmt.Fprint(w, `{"nodes":[{"name":"host-a","host_ip":"10.0.0.1","summary_data":[{"resource_type":"CPU","total":"16","allocated":"8","unallocated":"8","unit":"core"},{"resource_type":"MEMORY","total":"32","allocated":"16","unallocated":"16","unit":"GiB"}]}],"total_size":1}`)
				case strings.HasSuffix(r.URL.Path, "/queues/queue-test"):
					if test.quotaDenied {
						w.WriteHeader(403)
						return
					}
					fmt.Fprint(w, `{"properties":{"quota":[{"resource_type":"CPU","unit":"core","total":"4","allocated":"4","unallocated":"0"}]}}`)
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			queue := SSPQueueItem{Name: "queue-test", Type: "EXCLUSIVE", Cluster: "cluster", Subscription: "sub", ResourceGroup: "default", Region: "cn-pj-01", Profile: "test"}
			result, err := NewSSPResourceService(nil, client).getQueueNodeUsageWithQuota(context.Background(), queue)
			if (err != nil) != (test.quotaDenied || test.nodesDenied) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if test.quotaDenied && test.nodesDenied {
				if result != nil {
					t.Fatal(result)
				}
				return
			}
			if result == nil || result.IsQueryComplete() != (err == nil) || result.Scope != queueUsageScopeNodes {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if (result.Quota == nil) != test.quotaDenied || (len(result.Items) == 0) != test.nodesDenied {
				t.Fatalf("partial data lost: %+v", result)
			}
			if err != nil && (strings.Contains(err.Error(), "http://") || !strings.Contains(err.Error(), "HTTP 403")) {
				t.Fatal(err)
			}
			if !test.nodesDenied && result.NodeCount != 1 {
				t.Fatal(result)
			}
		})
	}
}

func TestQueueFreeFilterDoesNotSpendOrHideQuota(t *testing.T) {
	quota := &platform.SSPQueueQuota{Resources: []platform.SSPResourceSummary{{ResourceType: "CPU", Total: "128", Allocated: "128", Unallocated: "0"}}}
	result := &SSPQueueNodeUsageResult{SharedVCPool: true, Scope: queueUsageScopeSharedVC, Quota: quota, Ownership: &SSPQueueOwnershipSummary{OwnedNodes: 2, EligibleNodes: 2}, Items: []SSPQueueNodeUsageItem{
		{HostName: "partial", Ownership: queueNodeOwned, CPUTotal: "16", CPUAllocated: "8", MemoryTotal: "32GiB", MemoryAllocated: "16GiB", AcceleratorTotal: 8, AcceleratorFree: 1},
		{HostName: "full", Ownership: queueNodeOwned, CPUTotal: "16", CPUAllocated: "16", MemoryTotal: "32GiB", MemoryAllocated: "16GiB", AcceleratorTotal: 8, AcceleratorFree: 1},
	}}
	before := append([]platform.SSPResourceSummary(nil), quota.Resources...)
	result.FilterFreeNodes()
	result.FilterFreeNodes()
	if len(result.Items) != 1 || result.Items[0].HostName != "partial" || result.NodeCount != 2 || !result.FreeOnly || !reflect.DeepEqual(quota.Resources, before) {
		t.Fatalf("filter changed scope or quota: %+v", result)
	}
	// Zero queue CPU does not turn --free into a scheduling/admission check.
	if result.Quota.Resources[0].Unallocated != "0" || !result.IsQueryComplete() {
		t.Fatal(result)
	}
}

func TestQueueNodeUsageCancellationIsNotAnEmptyResult(t *testing.T) {
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewSSPResourceService(nil, client).getQueueNodeUsageWithQuota(ctx, SSPQueueItem{Name: "q", Cluster: "c", Subscription: "sub", Region: "cn-pj-01", Profile: "test"})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
