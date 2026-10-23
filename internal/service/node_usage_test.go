package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"rayctl/internal/platform"
)

const (
	usageVCUID = "019fc69a-98d5-70b4-9745-957244ee092f"
	otherVCUID = "019fc69a-98d5-70b4-9745-957244ee092e"
)

type nodeUsagePlatformFake struct {
	clusters []platform.SSPCluster
	err      error
	calls    int
	region   string
}

func (f *nodeUsagePlatformFake) ListSSPClustersForNodeUsage(_ context.Context, region string) ([]platform.SSPCluster, error) {
	f.calls++
	f.region = region
	return f.clusters, f.err
}

func usageSSPCluster(name, uid, sourceName, sourceUID, state string) platform.SSPCluster {
	cluster := platform.SSPCluster{Name: name, UID: uid, State: state}
	cluster.Properties.Source.Name = sourceName
	cluster.Properties.Source.UID = sourceUID
	return cluster
}

func TestNodeUsageFromLabels(t *testing.T) {
	for _, test := range []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{name: "ECP private", labels: map[string]string{prodRoleLabelKey: "ecp-private"}, want: "ECP"},
		{name: "ECS", labels: map[string]string{prodRoleLabelKey: "ecs"}, want: "ECS"},
		{name: "explicit SSP", labels: map[string]string{prodRoleLabelKey: " SSP "}, want: "SSP"},
		{name: "queue name overrides product", labels: map[string]string{prodRoleLabelKey: "ecs", nodeQueueNameLabelKey: "queue-demo"}, want: "SSP"},
		{name: "queue UID", labels: map[string]string{prodRoleLabelKey: "ecp-private", nodeQueueUIDLabelKey: "queue-id"}, want: "SSP"},
		{name: "SSP cluster name", labels: map[string]string{prodRoleLabelKey: "ecp-private", sspClusterNameLabel: "cluster-demo"}, want: "SSP"},
		{name: "SSP cluster UID", labels: map[string]string{nodeSSPClusterUIDLabelKey: "cluster-id"}, want: "SSP"},
		{name: "VC alone is ECP", labels: map[string]string{prodRoleLabelKey: "ecp-private", "resource.compute.sensecore.cn/vc-uid": usageVCUID}, want: "ECP"},
		{name: "unknown generic role", labels: map[string]string{genericRoleLabelPrefix + "master": ""}, want: "master"},
		{name: "substring is not SSP", labels: map[string]string{prodRoleLabelKey: "ssp-controller"}, want: "ssp-controller"},
		{name: "empty assignment", labels: map[string]string{prodRoleLabelKey: "ecs", nodeQueueNameLabelKey: " - ", nodeSSPClusterUIDLabelKey: " "}, want: "ECS"},
		{name: "nil labels", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nodeUsageFromLabels(test.labels); got != test.want {
				t.Fatalf("nodeUsageFromLabels() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNodeListUsageAliasesDoNotFilterBeforePlatformEnrichment(t *testing.T) {
	for _, target := range []string{"ecp", "ecs", "SSP"} {
		t.Run(target, func(t *testing.T) {
			client := fake.NewSimpleClientset(
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "old-product", Labels: map[string]string{prodRoleLabelKey: "ecp-private"}}},
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "queue-node", Labels: map[string]string{
					prodRoleLabelKey: "ecs", sspClusterNameLabel: "cluster-demo", nodeSSPClusterUIDLabelKey: "cluster-id",
				}}},
			)
			nodes, selector, err := NewNodeService(client).List(context.Background(), target, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(nodes) != 2 || selector != "usage="+strings.ToUpper(target) {
				t.Fatalf("List() = %+v, %q", nodes, selector)
			}
			if nodes[1].Usage != "SSP" || nodes[1].ProdRole != "ecs" || nodes[1].SSPClusterName != "cluster-demo" || nodes[1].SSPClusterUID != "cluster-id" {
				t.Fatalf("SSP label projection = %+v", nodes[1])
			}
			actions := client.Actions()
			if len(actions) != 1 || actions[0].GetResource().Resource != "nodes" {
				t.Fatalf("unexpected API dependencies: %+v", actions)
			}
			if got := actions[0].(ktesting.ListAction).GetListRestrictions().Labels.String(); got != "" {
				t.Fatalf("alias prematurely applied Kubernetes label selector %q", got)
			}
		})
	}
}

func TestNodeListRawSelectorsStayUnchanged(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "selected", Labels: map[string]string{prodRoleLabelKey: "ecp-private", nodeQueueNameLabelKey: "queue-demo"}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "other", Labels: map[string]string{prodRoleLabelKey: "ecs"}}},
	)
	raw := prodRoleLabelKey + "=ecp-private"
	nodes, selector, err := NewNodeService(client).List(context.Background(), raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Name != "selected" || nodes[0].Usage != "SSP" || selector != raw {
		t.Fatalf("List(raw selector) = %+v, %q", nodes, selector)
	}
	if got := FilterNodeListByUsage(nodes, raw); !reflect.DeepEqual(got, nodes) {
		t.Fatalf("raw selector was filtered by usage: %+v", got)
	}
}

func TestNodeUsageSourceIdentity(t *testing.T) {
	for _, test := range []struct {
		name    string
		node    NodeListItem
		cluster platform.SSPCluster
		want    bool
	}{
		{name: "UID wins when name changed", node: NodeListItem{ClusterName: "vc-new", ClusterUID: usageVCUID}, cluster: usageSSPCluster("cluster", "", "vc-old", usageVCUID, "RUNNING"), want: true},
		{name: "UID conflict prevents name fallback", node: NodeListItem{ClusterName: "vc-demo", ClusterUID: usageVCUID}, cluster: usageSSPCluster("cluster", "", "vc-demo", otherVCUID, "RUNNING")},
		{name: "node name only", node: NodeListItem{ClusterName: "vc-demo"}, cluster: usageSSPCluster("cluster", "", "vc-demo", usageVCUID, "RUNNING"), want: true},
		{name: "source name only", node: NodeListItem{ClusterName: "vc-demo", ClusterUID: usageVCUID}, cluster: usageSSPCluster("cluster", "", "vc-demo", "", "RUNNING"), want: true},
		{name: "legacy derived name is not UID", node: NodeListItem{ClusterName: "vc-demo", ClusterUID: "demo"}, cluster: usageSSPCluster("cluster", "", "vc-demo", usageVCUID, "RUNNING"), want: true},
		{name: "name is exact", node: NodeListItem{ClusterName: "vc-demo"}, cluster: usageSSPCluster("cluster", "", "vc-demo-longer", "", "RUNNING")},
		{name: "no source", node: NodeListItem{ClusterName: "vc-demo", ClusterUID: usageVCUID}, cluster: usageSSPCluster("cluster", "", "", "", "RUNNING")},
		{name: "no node identity", cluster: usageSSPCluster("cluster", "", "vc-demo", usageVCUID, "RUNNING")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nodeMatchesSSPClusterSource(test.node, test.cluster); got != test.want {
				t.Fatalf("nodeMatchesSSPClusterSource() = %t, want %t", got, test.want)
			}
		})
	}

	cluster := usageSSPCluster("cluster", "", "vc-demo", "", "RUNNING")
	cluster.Properties.Source.ID = otherVCUID
	if nodeMatchesSSPClusterSource(NodeListItem{ClusterName: "vc-demo", ClusterUID: usageVCUID}, cluster) {
		t.Fatal("UUID Source.ID conflict fell back to name")
	}
	cluster.Properties.Source.ID = "/subscriptions/sub/virtualClusters/vc-other"
	if !nodeMatchesSSPClusterSource(NodeListItem{ClusterName: "vc-demo", ClusterUID: usageVCUID}, cluster) {
		t.Fatal("non-UUID Source.ID should not block matching Source.Name")
	}
	cluster.Properties.Source.ID = "/subscriptions/sub/virtualClusters/vc-" + otherVCUID
	if nodeMatchesSSPClusterSource(NodeListItem{ClusterName: "vc-demo", ClusterUID: usageVCUID}, cluster) {
		t.Fatal("UUID Source.ID path conflict fell back to name")
	}
	cluster.Properties.Source.ID = "/subscriptions/sub/virtualClusters/" + usageVCUID
	if !nodeMatchesSSPClusterSource(NodeListItem{ClusterUID: "vc-" + usageVCUID}, cluster) {
		t.Fatal("UUID Source.ID path did not match prefixed node UID")
	}
}

func TestEnrichNodeListUsageGroupsClustersAndPreservesVC(t *testing.T) {
	nodes := []NodeListItem{
		{Name: "converted", Usage: "ECP", ProdRole: "ecp-private", ClusterName: "vc-demo", ClusterUID: usageVCUID},
		{Name: "old-vc", Usage: "ECP", ProdRole: "ecp-private", ClusterName: "vc-other", ClusterUID: otherVCUID},
		{Name: "bare", Usage: "ECS", ProdRole: "ecs"},
	}
	client := &nodeUsagePlatformFake{clusters: []platform.SSPCluster{
		usageSSPCluster("cluster-z", "z-uid", "vc-demo", usageVCUID, "RUNNING"),
		usageSSPCluster("cluster-a", "a-uid", "vc-demo", usageVCUID, "PENDING"),
		usageSSPCluster("cluster-z", "z-uid", "vc-demo", usageVCUID, "RUNNING"),
		usageSSPCluster("cluster-deleted", "deleted-uid", "vc-other", otherVCUID, "DELETED"),
		usageSSPCluster("cluster-deleting", "deleting-uid", "vc-other", otherVCUID, "deleting"),
	}}
	if err := EnrichNodeListUsage(context.Background(), nodes, client, "cn-pj-01"); err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || client.region != "cn-pj-01" {
		t.Fatalf("platform lookup calls = %d, region = %q", client.calls, client.region)
	}
	if nodes[0].Usage != "SSP" || nodes[0].SSPClusterName != "cluster-a, cluster-z" || nodes[0].SSPClusterUID != "a-uid, z-uid" {
		t.Fatalf("SSP association = %+v", nodes[0])
	}
	if nodes[0].ProdRole != "ecp-private" || nodes[0].ClusterName != "vc-demo" || nodes[0].ClusterUID != usageVCUID {
		t.Fatalf("raw product/VC association overwritten: %+v", nodes[0])
	}
	if nodes[1].Usage != "ECP" || nodes[1].SSPClusterName != "" || nodes[2].Usage != "ECS" {
		t.Fatalf("unassociated nodes changed: %+v", nodes)
	}
	if got := FilterNodeListByUsage(nodes, "ssp"); len(got) != 1 || got[0].Name != "converted" {
		t.Fatalf("SSP filter = %+v", got)
	}
	if got := FilterNodeListByUsage(nodes, "ecp"); len(got) != 1 || got[0].Name != "old-vc" {
		t.Fatalf("ECP filter = %+v", got)
	}
	if got := FilterNodeListByUsage(nodes, "ecs"); len(got) != 1 || got[0].Name != "bare" {
		t.Fatalf("ECS filter = %+v", got)
	}
}

func TestEnrichNodeListUsageFailurePreservesInput(t *testing.T) {
	nodes := []NodeListItem{{Name: "node", Usage: "ECP", ProdRole: "ecp-private", ClusterName: "vc-demo", ClusterUID: usageVCUID}}
	want := append([]NodeListItem(nil), nodes...)
	lookupErr := errors.New("platform unavailable")
	client := &nodeUsagePlatformFake{err: lookupErr, clusters: []platform.SSPCluster{
		usageSSPCluster("partial", "partial-uid", "vc-demo", usageVCUID, "RUNNING"),
	}}
	if err := EnrichNodeListUsage(context.Background(), nodes, client, "cn-pj-01"); !errors.Is(err, lookupErr) {
		t.Fatalf("EnrichNodeListUsage error = %v, want lookup error", err)
	}
	if !reflect.DeepEqual(nodes, want) {
		t.Fatalf("platform failure changed input: %+v", nodes)
	}
	if err := EnrichNodeListUsage(context.Background(), nodes, nil, ""); err != nil {
		t.Fatalf("nil client: %v", err)
	}
	if err := EnrichNodeListUsage(context.Background(), nil, client, ""); err != nil || client.calls != 1 {
		t.Fatalf("empty node list queried platform: calls=%d, error=%v", client.calls, err)
	}
}

func TestEnrichNodeListUsageNameFallbackRequiresUniqueSource(t *testing.T) {
	cluster := func(name, sourceUID, profile, state string) platform.SSPCluster {
		item := usageSSPCluster(name, name+"-uid", "vc-demo", sourceUID, state)
		item.ProfileName = profile
		return item
	}
	for _, test := range []struct {
		name     string
		clusters []platform.SSPCluster
		want     string
	}{
		{name: "different source UIDs", clusters: []platform.SSPCluster{
			cluster("cluster-a", usageVCUID, "d", "RUNNING"),
			cluster("cluster-b", otherVCUID, "d", "RUNNING"),
		}},
		{name: "unknown UIDs in different profiles", clusters: []platform.SSPCluster{
			cluster("cluster-a", "", "d", "RUNNING"),
			cluster("cluster-b", "", "pt", "RUNNING"),
		}},
		{name: "known and unknown identity", clusters: []platform.SSPCluster{
			cluster("cluster-a", usageVCUID, "d", "RUNNING"),
			cluster("cluster-b", "", "d", "RUNNING"),
		}},
		{name: "same UID in multiple clusters", clusters: []platform.SSPCluster{
			cluster("cluster-b", usageVCUID, "pt", "RUNNING"),
			cluster("cluster-a", usageVCUID, "d", "RUNNING"),
		}, want: "cluster-a, cluster-b"},
		{name: "same profile without source UID", clusters: []platform.SSPCluster{
			cluster("cluster-b", "", "d", "RUNNING"),
			cluster("cluster-a", "", "d", "RUNNING"),
		}, want: "cluster-a, cluster-b"},
		{name: "deleted collision is ignored", clusters: []platform.SSPCluster{
			cluster("cluster-a", usageVCUID, "d", "RUNNING"),
			cluster("cluster-b", otherVCUID, "pt", "DELETED"),
		}, want: "cluster-a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			nodes := []NodeListItem{{Name: "node", Usage: "ECP", ProdRole: "ecp-private", ClusterName: "vc-demo"}}
			if err := EnrichNodeListUsage(context.Background(), nodes, &nodeUsagePlatformFake{clusters: test.clusters}, ""); err != nil {
				t.Fatal(err)
			}
			wantUsage := "ECP"
			if test.want != "" {
				wantUsage = "SSP"
			}
			if nodes[0].Usage != wantUsage || nodes[0].SSPClusterName != test.want {
				t.Fatalf("name-only association = %+v, want usage %s and clusters %q", nodes[0], wantUsage, test.want)
			}
		})
	}
}

func TestEnrichNodeListUsageUIDMatchSurvivesAmbiguousName(t *testing.T) {
	clusters := []platform.SSPCluster{
		usageSSPCluster("cluster-a", "a-uid", "vc-demo", usageVCUID, "RUNNING"),
		usageSSPCluster("cluster-b", "b-uid", "vc-demo", otherVCUID, "RUNNING"),
	}
	clusters[0].ProfileName = "d"
	clusters[1].ProfileName = "pt"
	nodes := []NodeListItem{{Name: "node", Usage: "ECP", ProdRole: "ecp-private", ClusterName: "vc-demo", ClusterUID: usageVCUID}}
	if err := EnrichNodeListUsage(context.Background(), nodes, &nodeUsagePlatformFake{clusters: clusters}, ""); err != nil {
		t.Fatal(err)
	}
	if nodes[0].Usage != "SSP" || nodes[0].SSPClusterName != "cluster-a" {
		t.Fatalf("authoritative UID association = %+v", nodes[0])
	}
}
