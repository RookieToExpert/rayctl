package service

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rayctl/internal/platform"
)

func TestFilterAIDPods(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "wanted", Labels: map[string]string{sspWorkloadUIDLabel: "uid-1"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "other", Labels: map[string]string{sspWorkloadNameLabel: "other"}}},
	}
	result := filterAIDPods(pods, platform.SSPAID{UID: "uid-1", Name: "dev-demo"})
	if len(result) != 1 || result[0].Name != "wanted" {
		t.Fatalf("unexpected pods: %#v", result)
	}
}

func TestAIDDisplayHelpers(t *testing.T) {
	value := true
	if got := boolPointerText(&value); got != "Y" {
		t.Fatalf("boolPointerText(true) = %q", got)
	}
	if got := endpointText("10.140.80.10", "49225"); got != "10.140.80.10:49225" {
		t.Fatalf("endpointText() = %q", got)
	}
	if got := lastResourceSegment("/clusters/cluster-t/queues/queue-t-reserved"); got != "queue-t-reserved" {
		t.Fatalf("lastResourceSegment() = %q", got)
	}
}

func TestFormatSSPAIDResourceSummary(t *testing.T) {
	got := formatSSPAIDResourceSummary(SSPAIDResourceItem{
		CPU:         "14",
		Memory:      "240Gi",
		Accelerator: "1",
		GPUModel:    "A800",
		MachineType: "n3ls.ii.i60a",
	})
	want := "14 CPU / 240Gi Memory / 1 A800 / n3ls.ii.i60a Machine Type"
	if got != want {
		t.Fatalf("formatSSPAIDResourceSummary() = %q, want %q", got, want)
	}
}
