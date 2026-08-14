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
