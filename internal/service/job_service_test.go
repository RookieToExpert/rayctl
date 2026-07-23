package service

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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
