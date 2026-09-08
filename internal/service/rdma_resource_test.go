package service

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKubernetesNodeRDMAUsageUsesAllocatableAndActivePodRequests(t *testing.T) {
	nodes := []corev1.Node{{
		ObjectMeta: metav1.ObjectMeta{Name: "host-a"},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{corev1.ResourceName("rdma/hca"): resource.MustParse("8")},
			Addresses:   []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.1"}},
		},
	}}
	pods := []corev1.Pod{
		{
			Spec: corev1.PodSpec{
				NodeName: "host-a",
				Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceName("rdma/hca"): resource.MustParse("1")},
				}}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		{
			Spec: corev1.PodSpec{
				NodeName: "host-a",
				Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceName("rdma-training/roce"): resource.MustParse("1")},
				}}},
			},
			Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
		},
	}

	usage, ok := lookupNodeRDMAUsage(kubernetesNodeRDMAUsage(nodes, pods), "", "10.0.0.1")
	if !ok {
		t.Fatal("RDMA usage was not found by node IP")
	}
	allocated, total := rdmaUsageStrings(usage)
	if allocated != "1" || total != "8" {
		t.Fatalf("RDMA usage = %s/%s, want 1/8", allocated, total)
	}
}
