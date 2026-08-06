package service

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

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

func TestResolveAIDRegionFromNodeZone(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "host-10-140-217-17",
		Labels: map[string]string{sspNodeZoneLabel: "cn-pj-01a"},
	}})
	service := &SSPAIDService{clientset: clientset}
	pods := []corev1.Pod{{Spec: corev1.PodSpec{NodeName: "host-10-140-217-17"}}}
	if got := service.resolveAIDRegion(context.Background(), "", pods); got != "cn-pj-01" {
		t.Fatalf("resolveAIDRegion() = %q, want cn-pj-01", got)
	}
}

func TestResolveAIDRegionFromHCWithoutPods(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "pt-node",
		Labels: map[string]string{sspNodeZoneLabel: "cn-pj-03a"},
	}})
	service := &SSPAIDService{clientset: clientset}
	if got := service.resolveAIDRegion(context.Background(), "", nil); got != "cn-pj-03" {
		t.Fatalf("resolveAIDRegion() = %q, want cn-pj-03", got)
	}
}

func TestRegionFromSSPZone(t *testing.T) {
	for input, want := range map[string]string{
		"cn-pj-01a": "cn-pj-01",
		"cn-pj-03b": "cn-pj-03",
		"cn-pj-01":  "cn-pj-01",
	} {
		if got := regionFromSSPZone(input); got != want {
			t.Fatalf("regionFromSSPZone(%q) = %q, want %q", input, got, want)
		}
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
