package service

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
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
