package podevidence

import (
	corev1 "k8s.io/api/core/v1"
	"testing"
)

func TestContainerMountsRetainPVCMemoryAndReadonlySubpath(t *testing.T) {
	pod := corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "afs-data"}}}, {Name: "shm", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}}}}}
	container := corev1.Container{VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data", SubPath: "train", ReadOnly: true}, {Name: "shm", MountPath: "/dev/shm"}}}
	mounts := containerMounts(pod, container)
	if len(mounts) != 2 || mounts[0].Source != "afs-data" || !mounts[0].ReadOnly || mounts[0].SubPath != "train" || mounts[1].Type != "emptyDir" || mounts[1].Source != "Memory" {
		t.Fatal(mounts)
	}
}
