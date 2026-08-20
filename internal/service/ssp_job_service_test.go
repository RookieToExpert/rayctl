package service

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"rayctl/internal/platform"
)

func TestDetectWorkloadTypeByWorkloadNameAndPodName(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "demo-worker-0",
		Labels: map[string]string{
			sspWorkloadTypeLabel: SSPWorkloadTypeTrainingJob,
			sspWorkloadNameLabel: "demo",
		},
	}}
	ecpPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:   "ecp-demo-worker-0",
		Labels: map[string]string{"volcano.sh/job-name": "ecp-demo"},
	}}
	service := NewSSPJobService(fake.NewSimpleClientset(pod, ecpPod), nil)

	for _, identifier := range []string{"demo", "demo-worker-0"} {
		got, err := service.DetectWorkloadType(t.Context(), identifier)
		if err != nil {
			t.Fatalf("DetectWorkloadType(%q) error = %v", identifier, err)
		}
		if got != SSPWorkloadTypeTrainingJob {
			t.Fatalf("DetectWorkloadType(%q) = %q", identifier, got)
		}
	}
	got, err := service.DetectWorkloadType(t.Context(), "ecp-demo")
	if err != nil {
		t.Fatalf("DetectWorkloadType(ecp-demo) error = %v", err)
	}
	if got != WorkloadTypeECPVCJob {
		t.Fatalf("DetectWorkloadType(ecp-demo) = %q", got)
	}
}

func TestSummarizeSSPSchedulingFailure(t *testing.T) {
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-master-0"},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  corev1.PodReasonUnschedulable,
			Message: "0/10 nodes are available: 4 Insufficient cpu, 3 Insufficient nvidia.com/gpu, 3 node(s) didn't match Pod's node affinity/selector",
		}}},
	}}

	reason, detail := summarizeSSPSchedulingFailure(pods)
	for _, expected := range []string{"CPU 不足", "加速卡不足", "节点亲和性"} {
		if !strings.Contains(reason, expected) {
			t.Fatalf("reason %q does not contain %q", reason, expected)
		}
	}
	if !strings.Contains(detail, "Insufficient cpu") {
		t.Fatalf("unexpected detail: %q", detail)
	}
}

func TestNormalizeSSPJobState(t *testing.T) {
	tests := map[string]string{
		"TRAINING_JOB_STATE_PENDING": "Pending",
		"JOB_RUNNING":                "Running",
		"SUCCEEDED":                  "Succeeded",
		"":                           "-",
	}
	for input, want := range tests {
		if got := normalizeSSPJobState(input); got != want {
			t.Errorf("normalizeSSPJobState(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveJobRegionDefaultsToD(t *testing.T) {
	service := NewSSPJobService(fake.NewSimpleClientset(), nil)
	if got := service.resolveJobRegion(t.Context(), nil); got != "cn-pj-01" {
		t.Fatalf("resolveJobRegion() = %q, want cn-pj-01", got)
	}
}

func TestResolveJobRegionUsesDetectedPodNode(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "host-pt",
		Labels: map[string]string{sspNodeZoneLabel: "cn-pj-03a"},
	}}
	pods := []corev1.Pod{{Spec: corev1.PodSpec{NodeName: node.Name}}}
	service := NewSSPJobService(fake.NewSimpleClientset(node), nil)
	if got := service.resolveJobRegion(t.Context(), pods); got != "cn-pj-03" {
		t.Fatalf("resolveJobRegion() = %q, want cn-pj-03", got)
	}
}

func TestFilterSSPPodsForJobPrefersUID(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "wanted", Labels: map[string]string{sspWorkloadUIDLabel: "uid-1"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "other", Labels: map[string]string{sspWorkloadNameLabel: "other-job"}}},
	}
	job := platform.SSPTrainingJob{UID: "uid-1", Name: "demo"}
	result := filterSSPPodsForJob(pods, job)
	if len(result) != 1 || result[0].Name != "wanted" {
		t.Fatalf("unexpected pods: %#v", result)
	}
}

func TestMakeSSPPodResourceItemsMatchesTask(t *testing.T) {
	task := platform.SSPTrainingJobTask{Name: "worker"}
	task.ResourceSpec.CPUCount = 14
	task.ResourceSpec.MemoryGiB = 240
	task.ResourceSpec.AccelerateDeviceCount = 1
	task.ResourceSpec.AccelerateDeviceModel = "A800"
	task.ResourceSpec.MachineTypes = []string{"n3ls.ii.i60a"}
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-worker-0", Labels: map[string]string{"volcano.sh/task-spec": "worker"}},
		Spec:       corev1.PodSpec{NodeName: "host-1"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}}

	items, nodes := makeSSPPodResourceItems(pods, []platform.SSPTrainingJobTask{task})
	if len(items) != 1 || items[0].CPU != "14" || items[0].Memory != "240Gi" || items[0].Model != "A800" {
		t.Fatalf("unexpected pod resources: %#v", items)
	}
	if len(nodes) != 1 || nodes[0] != "host-1" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
}

func TestMakeSSPPodResourceItemsFallsBackToPodMachineType(t *testing.T) {
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-worker-0"},
		Spec: corev1.PodSpec{
			NodeName:     "host-1",
			NodeSelector: map[string]string{sspMachineTypeLabel: "h2ls.ru.k10"},
		},
	}}

	items, _ := makeSSPPodResourceItems(pods, nil)
	if len(items) != 1 || items[0].MachineType != "h2ls.ru.k10" {
		t.Fatalf("unexpected pod resources: %#v", items)
	}
}

func TestEnrichSSPPodMachineTypesFallsBackToAssignedNode(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "host-1",
		Labels: map[string]string{sspMachineTypeLabel: "h2ls.ru.k10"},
	}}
	service := NewSSPJobService(fake.NewSimpleClientset(node), nil)
	items := service.enrichSSPPodMachineTypes(t.Context(), []SSPJobPodResourceItem{{Pod: "demo-0", Node: "host-1"}})
	if len(items) != 1 || items[0].MachineType != "h2ls.ru.k10" {
		t.Fatalf("unexpected enriched resources: %#v", items)
	}
}

func TestEnrichSSPVolumeClaimsUsesGeneratedPVCIndex(t *testing.T) {
	claims := []VolumeClaimRef{
		{ClaimName: "ait-pvc-job-0", Status: "Bound", FrontendVolume: "-"},
		{ClaimName: "ait-pvc-job-1", Status: "Bound"},
	}
	mounts := []sspVolumeDescriptor{
		{Type: "PV_OCEANSTOR", Name: "afs-demo"},
		{Type: "PV_AOSS", Name: "bucket-a", Endpoint: "https://aoss.example.com"},
	}

	result := enrichSSPVolumeClaims(claims, mounts)
	if result[0].FrontendVolume != "afs-demo" {
		t.Fatalf("unexpected AFS mapping: %#v", result[0])
	}
	if result[1].FrontendVolume != "https://aoss.example.com/bucket-a" {
		t.Fatalf("unexpected AOSS mapping: %#v", result[1])
	}
}
