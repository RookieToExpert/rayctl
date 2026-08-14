package service

import (
	"testing"

	"rayctl/internal/platform"
)

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
