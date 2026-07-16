package service

import (
	"context"
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
