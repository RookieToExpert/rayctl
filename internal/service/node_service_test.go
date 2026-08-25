package service

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDescribeIncludesNodeReadyStatus(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "host-test"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	})

	result, err := NewNodeService(clientset).Describe(context.Background(), "host-test")
	if err != nil {
		t.Fatalf("Describe returned error: %v", err)
	}
	if result.Ready != "Ready" {
		t.Fatalf("Ready = %q, want Ready", result.Ready)
	}
}

func TestNodeReadyStatusReturnsNotReady(t *testing.T) {
	conditions := []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}
	if got := nodeReadyStatus(conditions); got != "NotReady" {
		t.Fatalf("nodeReadyStatus() = %q, want NotReady", got)
	}
}

func TestNVIDIAAcceleratorResources(t *testing.T) {
	resources := corev1.ResourceList{
		corev1.ResourceName(nvidiaGPUResourceName): resource.MustParse("8"),
	}
	if got := gpuCapacityValue(resources); got != 8 {
		t.Fatalf("gpuCapacityValue() = %d, want 8", got)
	}
	if got := gpuRequestValue(resources); got != 8 {
		t.Fatalf("gpuRequestValue() = %d, want 8", got)
	}
}

func TestNodeResourceUsageFormattingMatchesVCUsage(t *testing.T) {
	if got := formatCPUUsage(resource.MustParse("96"), resource.MustParse("127900m")); got != "96/127.9" {
		t.Fatalf("formatCPUUsage() = %q, want 96/127.9", got)
	}
	if got := formatMemoryUsage(resource.MustParse("1600Gi"), resource.MustParse("2108586112Ki")); got != "1600/2010.904GiB" {
		t.Fatalf("formatMemoryUsage() = %q, want 1600/2010.904GiB", got)
	}
}

func TestDescribeManyKeepsInputOrderAndIndividualErrors(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "host-1"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "host-2"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "host-3"}},
	)

	results := NewNodeService(clientset).DescribeMany(context.Background(), []string{"host-3", "host-1", "missing", "host-2"}, 3)
	if len(results) != 4 {
		t.Fatalf("DescribeMany returned %d results, want 4", len(results))
	}
	for index, want := range []string{"host-3", "host-1", "missing", "host-2"} {
		if results[index].Identifier != want {
			t.Fatalf("results[%d].Identifier = %q, want %q", index, results[index].Identifier, want)
		}
	}
	if results[2].Err == nil {
		t.Fatal("missing node result has nil error")
	}
}

func TestDescribeUnassignedNodeSkipsHostSystemPods(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "host-free"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "kube-system",
				Name:      "device-plugin",
				OwnerReferences: []metav1.OwnerReference{{
					Kind: "DaemonSet",
					Name: "device-plugin",
				}},
			},
			Spec:   corev1.PodSpec{NodeName: "host-free"},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)

	result, err := NewNodeService(clientset).Describe(context.Background(), "host-free")
	if err != nil {
		t.Fatalf("Describe returned error: %v", err)
	}
	if result.MatchedPodCount != 0 || len(result.Pods) != 0 {
		t.Fatalf("unassigned node pods = %v, want no user workloads", result.Pods)
	}
}

func TestDescribeKeepsUserJobAndFiltersSystemWorkloads(t *testing.T) {
	vcNamespace := "vc-example"
	resourceNamespace := "vcluster-user-default"
	clientset := fake.NewSimpleClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "host-user",
				Labels: map[string]string{nodeVClusterNamespaceLabelKey: vcNamespace},
			},
		},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: resourceNamespace,
			Labels: map[string]string{
				nsVClusterNamespaceLabelKey: vcNamespace,
				nsVirtualNameLabelKey:       "default",
			},
		}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "vcluster-user-system",
			Labels: map[string]string{
				nsVClusterNamespaceLabelKey: vcNamespace,
				nsVirtualNameLabelKey:       "kube-system",
			},
		}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: resourceNamespace, Name: "training-worker-0"},
			Spec: corev1.PodSpec{
				NodeName: "host-user",
				Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("8"),
				}}}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: resourceNamespace,
				Name:      "device-plugin",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: appsv1.SchemeGroupVersion.String(),
					Kind:       "DaemonSet",
					Name:       "device-plugin",
				}},
			},
			Spec:   corev1.PodSpec{NodeName: "host-user"},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "vcluster-user-system", Name: "controller"},
			Spec:       corev1.PodSpec{NodeName: "host-user"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)

	result, err := NewNodeService(clientset).Describe(context.Background(), "host-user")
	if err != nil {
		t.Fatalf("Describe returned error: %v", err)
	}
	if result.MatchedPodCount != 1 || len(result.Pods) != 1 || result.Pods[0] != "training-worker-0" {
		t.Fatalf("filtered pods = %v, want training-worker-0", result.Pods)
	}
	if result.CPUUsage != "8/0" {
		t.Fatalf("CPUUsage = %q, want 8/0", result.CPUUsage)
	}
}
