package service

import "testing"

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

func TestSubscriptionIDFromRID(t *testing.T) {
	rid := "/subscriptions/sub-id/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-test"
	if got := subscriptionIDFromRID(rid); got != "sub-id" {
		t.Fatalf("subscriptionIDFromRID() = %q, want sub-id", got)
	}
}
