package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckPVShowsObjectStorageEndpointAndBucket(t *testing.T) {
	storageClass := "quark-aoss-vcproxy-sc"
	clientset := fake.NewSimpleClientset(
		&corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-test"},
			Spec: corev1.PersistentVolumeSpec{
				StorageClassName: "csi-s3",
				ClaimRef: &corev1.ObjectReference{
					Namespace: "vcluster-test",
					Name:      "jfs-prod-pvc",
				},
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					CSI: &corev1.CSIPersistentVolumeSource{
						Driver:       "ru.yandex.s3.csi",
						VolumeHandle: "uid/discovery-prod",
					},
				},
			},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "vcluster-test",
				Name:      "jfs-prod-pvc",
				Annotations: map[string]string{
					"bucket":     "discovery-prod",
					"secretName": "jfs-gateway-s3",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &storageClass,
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "vcluster-test",
				Name:      "jfs-gateway-s3",
			},
			Data: map[string][]byte{
				"endpoint": []byte("https://aoss.example.com"),
			},
		},
	)

	result, err := NewStorageService(clientset, nil).CheckPV(context.Background(), "pvc-test")
	if err != nil {
		t.Fatalf("CheckPV returned error: %v", err)
	}
	if result.StorageType != "AOSS" {
		t.Fatalf("StorageType = %q, want AOSS", result.StorageType)
	}
	if result.AFSName != "https://aoss.example.com/discovery-prod" {
		t.Fatalf("AOSS location = %q, want endpoint with bucket", result.AFSName)
	}
}

func TestFormatObjectStorageLocationFallsBackToBucket(t *testing.T) {
	if got := formatObjectStorageLocation("", "discovery-prod"); got != "bucket=discovery-prod" {
		t.Fatalf("formatObjectStorageLocation() = %q", got)
	}
}

func TestFindVirtualPVCsUsesOnePVCPerNamespaceAndLimit(t *testing.T) {
	hostPV := "pvc-host"
	pvs := make([]corev1.PersistentVolume, 0, 20)
	for index := 0; index < 18; index++ {
		namespace := fmt.Sprintf("vcluster-%02d", index)
		pvs = append(pvs, corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("virtual-%02d", index), Labels: map[string]string{"source-pv": hostPV}},
			Spec:       corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: namespace, Name: "pvc-main"}},
		})
	}
	pvs = append(pvs, corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "duplicate", Labels: map[string]string{"source-pv": hostPV}},
		Spec:       corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "vcluster-00", Name: "pvc-second"}},
	})

	result := findVirtualPVCsForHostPVsInSnapshot(pvs, []string{hostPV}, 15)
	if len(result) != 15 {
		t.Fatalf("virtual pvc count = %d, want 15", len(result))
	}
	seenNamespace := make(map[string]struct{})
	for _, value := range result {
		namespace := strings.SplitN(value, "/", 2)[0]
		if _, exists := seenNamespace[namespace]; exists {
			t.Fatalf("namespace %q appears more than once", namespace)
		}
		seenNamespace[namespace] = struct{}{}
	}
}
