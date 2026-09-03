package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"

	"rayctl/internal/platform"
)

func TestEnsurePVCGetDiagnosisForPendingPVC(t *testing.T) {
	stage, diagnosis := ensurePVCGetDiagnosis("Pending", false, "", nil, []VolumeClaimRef{{
		ClaimName: "pvc-test",
		Status:    string(corev1.ClaimPending),
	}})
	if stage != "scheduling" {
		t.Fatalf("stage = %q, want scheduling", stage)
	}
	if len(diagnosis) != 1 || !strings.Contains(diagnosis[0], "AFS 的 AK/SK 错误") {
		t.Fatalf("diagnosis = %#v", diagnosis)
	}
}

func TestEnsurePVCGetDiagnosisPrioritizesMissingPVC(t *testing.T) {
	_, diagnosis := ensurePVCGetDiagnosis("Pending", false, "", nil, []VolumeClaimRef{
		{ClaimName: "pvc-pending", Status: string(corev1.ClaimPending)},
		{ClaimName: "pvc-missing", Status: "NotFound", Message: "PVC 在当前集群不存在"},
	})
	if len(diagnosis) != 1 || !strings.Contains(diagnosis[0], "当前集群不存在") {
		t.Fatalf("diagnosis = %#v", diagnosis)
	}
}

func TestEnsurePVCGetDiagnosisSkipsRunningJob(t *testing.T) {
	stage, diagnosis := ensurePVCGetDiagnosis("Running", false, "", nil, []VolumeClaimRef{{
		Status: string(corev1.ClaimPending),
	}})
	if stage != "" || len(diagnosis) != 0 {
		t.Fatalf("stage = %q, diagnosis = %#v", stage, diagnosis)
	}
}

func TestEnsurePVCGetDiagnosisDoesNotDuplicatePVCConclusion(t *testing.T) {
	existing := []string{"PVC 仍处于 Pending，当前大概率是 PVC 的 AKSK 错误。"}
	_, diagnosis := ensurePVCGetDiagnosis("Pending", false, "scheduling", existing, []VolumeClaimRef{{
		Status: string(corev1.ClaimPending),
	}})
	if len(diagnosis) != 1 || diagnosis[0] != existing[0] {
		t.Fatalf("diagnosis = %#v", diagnosis)
	}
}

func TestSelectDiagnosticPodsUsesSingleInspectPod(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "large-job-worker-1", Labels: map[string]string{"volcano.sh/task-spec": "worker"}},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "large-job-master-0", Labels: map[string]string{"volcano.sh/task-spec": "master"}},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "large-job-worker-0", Labels: map[string]string{"volcano.sh/task-spec": "worker"}},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		},
	}

	selected := selectDiagnosticPods(pods, "startup")
	if len(selected) != 1 {
		t.Fatalf("selected %d pods, want 1", len(selected))
	}
	if selected[0].Name != "large-job-master-0" {
		t.Fatalf("selected pod = %q, want master pod", selected[0].Name)
	}
}

func TestChooseMasterLogPodPrefersMaster(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "example-worker-0", Labels: map[string]string{"volcano.sh/task-spec": "worker"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "example-master-0", Labels: map[string]string{"volcano.sh/task-spec": "master"}}},
	}
	selected := chooseMasterLogPod(pods)
	if selected == nil || selected.Name != "example-master-0" {
		t.Fatalf("selected pod = %#v, want example-master-0", selected)
	}
}

func TestVolcanoJobAPIStateFilter(t *testing.T) {
	tests := []struct {
		name            string
		includeInactive bool
		status          string
		want            string
	}{
		{name: "default active", want: `(state="Running" OR state="Pending")`},
		{name: "pending", status: "pending", want: `state="Pending"`},
		{name: "running", status: "running", want: `state="Running"`},
		{name: "active", status: "active", want: `(state="Running" OR state="Pending")`},
		{name: "all status flag", includeInactive: true, want: ""},
		{name: "all filter", status: "all", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := volcanoJobAPIStateFilter(test.includeInactive, test.status); got != test.want {
				t.Fatalf("volcanoJobAPIStateFilter() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestVolcanoJobAPIFilterUsesDefaultNamespaceAndSubmitterUnion(t *testing.T) {
	want := `(namespace="default" OR submitter!="") AND (state="Running" OR state="Pending")`
	if got := volcanoJobAPIFilter(false, ""); got != want {
		t.Fatalf("volcanoJobAPIFilter() = %q, want %q", got, want)
	}

	wantAll := `(namespace="default" OR submitter!="")`
	if got := volcanoJobAPIFilter(true, ""); got != wantAll {
		t.Fatalf("volcanoJobAPIFilter(all) = %q, want %q", got, wantAll)
	}
}

func TestMatchingVirtualClustersPrefersExactName(t *testing.T) {
	clusters := []platform.VirtualCluster{
		{Name: "vc-a3-241ceshi-old", UID: "old"},
		{Name: "vc-a3-241ceshi", UID: "current", ProfileName: "ailabdev"},
	}

	matches := matchingVirtualClusters(clusters, "vc-a3-241ceshi")
	if len(matches) != 1 {
		t.Fatalf("matchingVirtualClusters() returned %d matches, want 1", len(matches))
	}
	if matches[0].UID != "current" || matches[0].ProfileName != "ailabdev" {
		t.Fatalf("matchingVirtualClusters() = %#v, want exact current-profile cluster", matches[0])
	}
}

func TestPlatformVCRefsUsesCanonicalUIDOnly(t *testing.T) {
	refs := platformVCRefs(platform.VirtualCluster{
		Name:        "vc-a3-241ceshi",
		DisplayName: "A3 test cluster",
		UID:         "019fc690-f9fa-780b-95c8-4eae54511b56",
	})
	if len(refs) != 1 || refs[0] != "vc-019fc690-f9fa-780b-95c8-4eae54511b56" {
		t.Fatalf("platformVCRefs() = %#v, want canonical vc UID only", refs)
	}
}

func TestExtractJobLifecycleTimes(t *testing.T) {
	created := time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC)
	job := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"phase": "Running", "lastTransitionTime": "2026-08-11T02:00:00Z"},
				map[string]any{"phase": "Completed", "lastTransitionTime": "2026-08-11T03:00:00Z"},
			},
		},
	}}
	job.SetCreationTimestamp(metav1.NewTime(created))

	createdAt, startedAt, endedAt := extractJobLifecycleTimes(job, nil, "Completed")
	if createdAt != "2026-08-11 09:00:00" {
		t.Fatalf("createdAt = %q", createdAt)
	}
	if startedAt != "2026-08-11 10:00:00" {
		t.Fatalf("startedAt = %q", startedAt)
	}
	if endedAt != "2026-08-11 11:00:00" {
		t.Fatalf("endedAt = %q", endedAt)
	}
}

func TestEnrichObjectStorageVolumeClaimRefIncludesBucket(t *testing.T) {
	storageClass := "quark-aoss-vcproxy-sc"
	clientset := fake.NewSimpleClientset(
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "vcluster-test",
				Name:      "jfs-prod-pvc",
				Annotations: map[string]string{
					"bucket":     "discovery-prod",
					"secretName": "jfs-gateway-s3",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &storageClass},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "vcluster-test",
				Name:      "jfs-gateway-s3",
			},
			Data: map[string][]byte{"endpoint": []byte("http://aoss.example.com")},
		},
	)
	service := &JobService{clientset: clientset}
	ref := VolumeClaimRef{ClaimName: "jfs-prod-pvc"}

	service.enrichObjectStorageVolumeClaimRef(context.Background(), "vcluster-test", &ref)

	if ref.FrontendVolume != "http://aoss.example.com/discovery-prod" {
		t.Fatalf("FrontendVolume = %q, want endpoint with bucket", ref.FrontendVolume)
	}
}

func TestEnrichObjectStorageVolumeClaimRefKeepsBucketWithoutSecret(t *testing.T) {
	storageClass := "quark-aoss-vcproxy-sc"
	clientset := fake.NewSimpleClientset(&corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "vcluster-test",
			Name:      "jfs-prod-pvc",
			Annotations: map[string]string{
				"bucket": "discovery-prod",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &storageClass},
	})
	service := &JobService{clientset: clientset}
	ref := VolumeClaimRef{ClaimName: "jfs-prod-pvc"}

	service.enrichObjectStorageVolumeClaimRef(context.Background(), "vcluster-test", &ref)

	if ref.FrontendVolume != "bucket=discovery-prod" {
		t.Fatalf("FrontendVolume = %q, want bucket fallback", ref.FrontendVolume)
	}
}

func TestLocateJobByUIDUsesPodOwnerReference(t *testing.T) {
	const jobUID = "0908d002-fc65-4291-a268-102360023265"
	clientset := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "vcluster-host-ns",
			Name:      "uid-fast-path-worker-0",
			Labels: map[string]string{
				"volcano.sh/job-name":            "uid-fast-path",
				"vcluster.loft.sh/vcluster-name": "vc-uid-fast-path",
				"lepton.sensetime.com/submitter": "tester",
				"scheduling.k8s.io/group-name":   "uid-fast-path-" + jobUID,
			},
			Annotations: map[string]string{
				"vcluster.loft.sh/namespace":        "default",
				"vcluster.loft.sh/owner-references": `[{"kind":"Job","name":"uid-fast-path","uid":"` + jobUID + `"}]`,
			},
		},
	})
	jobService := &JobService{clientset: clientset}

	identity, err := jobService.locateJobForPlatform(context.Background(), jobUID)
	if err != nil {
		t.Fatalf("locateJobForPlatform() error = %v", err)
	}
	if identity.Name != "uid-fast-path" {
		t.Fatalf("job name = %q, want uid-fast-path", identity.Name)
	}
	if identity.UID != jobUID {
		t.Fatalf("job UID = %q, want %s", identity.UID, jobUID)
	}
	if identity.VClusterName != "vc-uid-fast-path" {
		t.Fatalf("vcluster = %q, want vc-uid-fast-path", identity.VClusterName)
	}
}

func TestClassifyDockerLoginErrorPrioritizesRegistryAddress(t *testing.T) {
	err := fmt.Errorf(`Error response from daemon: Get "https://registry2.d.pjlab.org/v2/": dial tcp: lookup registry2.d.pjlab.org on 127.0.0.53:53: no such host`)
	status, message := classifyDockerLoginError(err, "registry2.d.pjlab.org")
	if status != "FAIL" {
		t.Fatalf("status = %q, want FAIL", status)
	}
	if !strings.HasPrefix(message, "镜像地址错误：") {
		t.Fatalf("message = %q, want address error first", message)
	}
	if !strings.Contains(message, "no such host") {
		t.Fatalf("message = %q, want original error", message)
	}
}

func TestClassifyDockerLoginErrorKeepsUnauthorizedAsCredentialFailure(t *testing.T) {
	err := fmt.Errorf("unauthorized: authentication required")
	status, message := classifyDockerLoginError(err, "registry2.d.pjlab.org.cn")
	if status != "FAIL" || message != err.Error() {
		t.Fatalf("status = %q, message = %q", status, message)
	}
}

func TestDockerRegistryHostname(t *testing.T) {
	tests := map[string]string{
		"registry2.d.pjlab.org.cn":     "registry2.d.pjlab.org.cn",
		"registry.example.com:5000":    "registry.example.com",
		"https://index.docker.io/v1/":  "index.docker.io",
		"registry.example.com/v2/path": "registry.example.com",
		"[2001:db8::1]:5000":           "2001:db8::1",
	}
	for registry, want := range tests {
		got, err := dockerRegistryHostname(registry)
		if err != nil {
			t.Fatalf("dockerRegistryHostname(%q) error = %v", registry, err)
		}
		if got != want {
			t.Fatalf("dockerRegistryHostname(%q) = %q, want %q", registry, got, want)
		}
	}
}

func TestClassifyDockerLoginTimeoutAsUnavailable(t *testing.T) {
	status, message := classifyDockerLoginError(fmt.Errorf("docker login: %w", context.DeadlineExceeded), "registry.example.com")
	if status != "ERROR" {
		t.Fatalf("status = %q, want ERROR", status)
	}
	if !strings.Contains(message, "暂时无法确认") {
		t.Fatalf("message = %q, want temporary verification failure", message)
	}
}

func TestIsSSPManagedWorkloadPod(t *testing.T) {
	for _, workloadType := range []string{SSPWorkloadTypeTrainingJob, SSPWorkloadTypeAID} {
		pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{sspWorkloadTypeLabel: workloadType}}}
		if !isSSPManagedWorkloadPod(pod) {
			t.Fatalf("workload type %q was not classified as SSP managed", workloadType)
		}
	}
	if isSSPManagedWorkloadPod(corev1.Pod{}) {
		t.Fatal("unlabelled ECP pod was classified as SSP managed")
	}
}
