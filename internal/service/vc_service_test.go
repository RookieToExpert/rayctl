package service

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"rayctl/internal/platform"
)

func TestEnrichVCNodeModelsUsesHCNodeLabels(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "host-10-140-217-110",
			Labels: map[string]string{
				"accelerator":                      "huawei-Ascend910",
				"accelerator-type":                 "module-910c-8",
				"node.kubernetes.io/npu.chip.name": "Ascend910",
			},
		},
	})
	items := []VCNodeListItem{{HostName: "host-10-140-217-110"}}

	(&VCService{clientset: clientset}).enrichVCNodeModels(context.Background(), items)

	if items[0].Model != "module-910c-8" {
		t.Fatalf("Model = %q, want module-910c-8", items[0].Model)
	}
}

func TestResolveVCNodesForRemovalMatchesExactIdentifiersAndDeduplicates(t *testing.T) {
	nodes := []VCNodeListItem{
		{UID: "uid-1", Name: "ecp-node-1", HostName: "host-10-0-0-1", HostIP: "10.0.0.1"},
		{UID: "uid-2", Name: "ecp-node-2", HostName: "host-10-0-0-2", HostIP: "10.0.0.2"},
	}
	selected, err := resolveVCNodesForRemoval([]string{"10.0.0.1", "uid-2", "host-10-0-0-1"}, nodes)
	if err != nil {
		t.Fatalf("resolveVCNodesForRemoval() error = %v", err)
	}
	if len(selected) != 2 || selected[0].UID != "uid-1" || selected[1].UID != "uid-2" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestResolveVCNodesForRemovalRejectsPartialOrForeignNode(t *testing.T) {
	nodes := []VCNodeListItem{{UID: "uid-1", HostIP: "10.0.0.1"}}
	if _, err := resolveVCNodesForRemoval([]string{"10.0.0"}, nodes); err == nil {
		t.Fatal("expected partial identifier to be rejected")
	}
}

func TestFilterFreeAcceleratorNodes(t *testing.T) {
	free := platform.VirtualClusterNodeResourceUsage{}
	free.Usage.Available.Device = "8"
	full := platform.VirtualClusterNodeResourceUsage{}
	full.Usage.Available.Device = "0"
	unknown := platform.VirtualClusterNodeResourceUsage{}
	unknown.Usage.Available.Device = "-"
	result := &VCResourceUsageResult{Items: []VCNodeResourceUsageItem{
		{HostName: "free", Usage: free},
		{HostName: "full", Usage: full},
		{HostName: "unknown", Usage: unknown},
	}}

	result.FilterFreeAcceleratorNodes()

	if len(result.Items) != 1 || result.Items[0].HostName != "free" {
		t.Fatalf("filtered items = %#v", result.Items)
	}
}

func TestSubscriptionIDFromRID(t *testing.T) {
	rid := "/subscriptions/sub-id/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-test"
	if got := subscriptionIDFromRID(rid); got != "sub-id" {
		t.Fatalf("subscriptionIDFromRID() = %q, want sub-id", got)
	}
}

func TestMatchVCIdentifierPrefersExactMatch(t *testing.T) {
	items := []VCListItem{
		{Name: "vc-a3-test", UID: "uid-1"},
		{Name: "vc-a3-test-long", UID: "uid-2"},
	}
	result, matched, err := matchVCIdentifier("vc-a3-test", items)
	if err != nil {
		t.Fatalf("matchVCIdentifier() error = %v", err)
	}
	if !matched || result == nil || result.UID != "uid-1" {
		t.Fatalf("result = %#v, matched = %t", result, matched)
	}
}

func TestMatchVCIdentifierRejectsAmbiguousFuzzyMatch(t *testing.T) {
	items := []VCListItem{
		{Name: "vc-a3-test-one", UID: "uid-1"},
		{Name: "vc-a3-test-two", UID: "uid-2"},
	}
	if _, matched, err := matchVCIdentifier("a3-test", items); !matched || err == nil {
		t.Fatalf("matched = %t, err = %v", matched, err)
	}
}
