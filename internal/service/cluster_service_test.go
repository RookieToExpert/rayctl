package service

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"rayctl/internal/platform"
)

type fakeClusterPlatform struct {
	cluster   *platform.VirtualCluster
	listCalls int
}

func (f *fakeClusterPlatform) FindExactVirtualCluster(context.Context, string) (*platform.VirtualCluster, error) {
	return f.cluster, nil
}

func (f *fakeClusterPlatform) ListCurrentProfileVirtualClusters(context.Context) ([]platform.VirtualCluster, error) {
	f.listCalls++
	return nil, nil
}

func (f *fakeClusterPlatform) ListVirtualClusters(context.Context) ([]platform.VirtualCluster, error) {
	f.listCalls++
	return nil, nil
}

func (f *fakeClusterPlatform) ResolveDisplayNamesWithProfiles(context.Context, []string) (map[string]string, map[string]string, error) {
	return map[string]string{}, map[string]string{}, nil
}

func TestClusterGetUsesTargetedNamespaceQueries(t *testing.T) {
	const (
		uid              = "019d28e0-9610-74ef-a722-9242dede9e37"
		controlNamespace = "vc-019d28e0-9610-74ef-a722-9242dede9e37"
	)
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: controlNamespace}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "vcluster-default",
			Labels: map[string]string{
				clusterVirtualNamespaceLabelKey: controlNamespace,
				clusterLogicalNamespaceLabelKey: "default",
			},
		}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "vcluster-system",
			Labels: map[string]string{
				clusterVirtualNamespaceLabelKey: controlNamespace,
				clusterLogicalNamespaceLabelKey: "kube-system",
			},
		}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "unrelated"}},
	)
	clusterService := NewClusterService(client, nil)

	result, err := clusterService.Get(context.Background(), "vc-"+uid)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result.ResourceNamespaceCount != 2 {
		t.Fatalf("ResourceNamespaceCount = %d, want 2", result.ResourceNamespaceCount)
	}

	listActions := 0
	getActions := 0
	for _, action := range client.Actions() {
		switch action.GetVerb() {
		case "get":
			getActions++
		case "list":
			listActions++
			listAction, ok := action.(k8stesting.ListAction)
			if !ok {
				t.Fatalf("list action type = %T", action)
			}
			wantSelector := clusterVirtualNamespaceLabelKey + "=" + controlNamespace
			if got := listAction.GetListRestrictions().Labels.String(); got != wantSelector {
				t.Fatalf("label selector = %q, want %q", got, wantSelector)
			}
		}
	}
	if getActions != 1 || listActions != 1 {
		t.Fatalf("get actions = %d, list actions = %d", getActions, listActions)
	}
}

func TestClusterGetExactNameDoesNotListPlatformClusters(t *testing.T) {
	const uid = "019d28e0-9610-74ef-a722-9242dede9e37"
	controlNamespace := "vc-" + uid
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: controlNamespace}},
	)
	platformClient := &fakeClusterPlatform{cluster: &platform.VirtualCluster{Name: "vc-test", UID: uid}}
	clusterService := NewClusterServiceWithPlatform(client, platformClient)

	result, err := clusterService.Get(context.Background(), "vc-test")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result.ClusterName != "vc-test" || result.ClusterUID != uid {
		t.Fatalf("result = %#v", result)
	}
	if platformClient.listCalls != 0 {
		t.Fatalf("platform list calls = %d, want 0", platformClient.listCalls)
	}
}
