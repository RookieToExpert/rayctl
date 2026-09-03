package cmd

import (
	"testing"

	"rayctl/internal/service"
)

func TestNormalizeNodeIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "IPv4", input: "10.140.214.222", want: "host-10-140-214-222"},
		{name: "IPv4 with spaces", input: " 10.12.138.28 ", want: "host-10-12-138-28"},
		{name: "host name", input: "host-10-140-214-222", want: "host-10-140-214-222"},
		{name: "custom node name", input: "worker-a", want: "worker-a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeNodeIdentifier(tt.input); got != tt.want {
				t.Fatalf("normalizeNodeIdentifier(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFilterNodeListByQueueMatchesHostAndIP(t *testing.T) {
	nodes := []service.NodeListItem{
		{Name: "host-10-1-1-1", InternalIP: "10.1.1.1"},
		{Name: "custom-node", InternalIP: "10.1.1.2"},
		{Name: "other", InternalIP: "10.1.1.3"},
	}
	queueNodes := &service.SSPQueueNodeListResult{
		Queue: service.SSPQueueItem{Name: "queue-demo"},
		Items: []service.VCNodeListItem{
			{HostName: "host-10-1-1-1"},
			{HostIP: "10.1.1.2"},
		},
	}
	got := filterNodeListByQueue(nodes, queueNodes)
	if len(got) != 2 || got[0].QueueName != "queue-demo" || got[1].Name != "custom-node" {
		t.Fatalf("filterNodeListByQueue() = %#v", got)
	}
}

func TestFilterNodeListByVCMatchesNameAndUID(t *testing.T) {
	nodes := []service.NodeListItem{
		{Name: "by-name", ClusterName: "vc-demo", ClusterUID: "uid-demo"},
		{Name: "by-uid", ClusterName: "vc-other", ClusterUID: "uid-target"},
		{Name: "other", ClusterName: "vc-other", ClusterUID: "uid-other"},
	}
	if got := filterNodeListByVC(nodes, "vc-demo"); len(got) != 1 || got[0].Name != "by-name" {
		t.Fatalf("filterNodeListByVC(name) = %#v", got)
	}
	if got := filterNodeListByVC(nodes, "vc-uid-target"); len(got) != 1 || got[0].Name != "by-uid" {
		t.Fatalf("filterNodeListByVC(uid) = %#v", got)
	}
}
