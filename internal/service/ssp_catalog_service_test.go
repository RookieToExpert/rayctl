package service

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFilterAndSortSSPCatalogItems(t *testing.T) {
	items := []SSPCatalogListItem{
		{Name: "old", State: "Running", CreatedAt: "2026-09-01 10:00:00"},
		{Name: "pending", State: "Pending", CreatedAt: "2026-09-02 11:00:00"},
		{Name: "new", State: "RUNNING", CreatedAt: "2026-09-02 12:00:00"},
	}
	got := filterAndSortSSPCatalogItems(items, "running", 1)
	if len(got) != 1 || got[0].Name != "new" {
		t.Fatalf("filterAndSortSSPCatalogItems() = %#v", got)
	}
}

func TestFilterAndSortFailedSSPCatalogItemsUsesEndedAt(t *testing.T) {
	items := []SSPCatalogListItem{
		{Name: "created-newer", State: "Failed", CreatedAt: "2026-09-18 12:00:00", EndedAt: "2026-09-18 12:10:00"},
		{Name: "failed-newer", State: "FAILED", CreatedAt: "2026-09-17 12:00:00", EndedAt: "2026-09-18 13:00:00"},
	}
	got := filterAndSortSSPCatalogItems(items, "failed", 1)
	if len(got) != 1 || got[0].Name != "failed-newer" {
		t.Fatalf("failed items = %#v, want failed-newer", got)
	}
}

func TestNormalizeSSPCatalogAPIState(t *testing.T) {
	if got := normalizeSSPCatalogAPIState(" Running "); got != "RUNNING" {
		t.Fatalf("normalizeSSPCatalogAPIState() = %q", got)
	}
}

func TestEnrichAIDCatalogNodesMatchesWorkloadUIDAndName(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "dev-by-uid-0",
			Labels: map[string]string{
				sspWorkloadTypeLabel: sspAIDWorkloadTypeValue,
				sspWorkloadUIDLabel:  "aid-uid",
			},
		}, Spec: corev1.PodSpec{NodeName: "host-a"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "dev-by-name-0",
			Labels: map[string]string{
				sspWorkloadTypeLabel: sspAIDWorkloadTypeValue,
				sspWorkloadNameLabel: "dev-by-name",
			},
		}, Spec: corev1.PodSpec{NodeName: "host-b"}},
	)
	result := &SSPCatalogListResult{Items: []SSPCatalogListItem{
		{UID: "aid-uid", Name: "dev-by-uid"},
		{Name: "dev-by-name"},
	}}
	service := &SSPCatalogService{clientset: clientset}

	service.enrichAIDCatalogNodes(t.Context(), result)

	if result.Items[0].Node != "host-a" || result.Items[1].Node != "host-b" {
		t.Fatalf("items = %#v", result.Items)
	}
}
