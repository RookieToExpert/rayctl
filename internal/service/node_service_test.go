package service

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
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
