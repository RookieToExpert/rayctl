package podevidence

import corev1 "k8s.io/api/core/v1"

func containerMounts(pod corev1.Pod, container corev1.Container) []Mount {
	volumes := map[string]corev1.Volume{}
	for _, volume := range pod.Spec.Volumes {
		volumes[volume.Name] = volume
	}
	result := []Mount{}
	for _, mount := range container.VolumeMounts {
		item := Mount{Name: mount.Name, Path: mount.MountPath, SubPath: mount.SubPath, ReadOnly: mount.ReadOnly, Type: "unknown"}
		volume := volumes[mount.Name]
		switch {
		case volume.PersistentVolumeClaim != nil:
			item.Type = "PVC"
			item.Source = volume.PersistentVolumeClaim.ClaimName
		case volume.HostPath != nil:
			item.Type = "hostPath"
			item.Source = volume.HostPath.Path
		case volume.EmptyDir != nil:
			item.Type = "emptyDir"
			item.Source = string(volume.EmptyDir.Medium)
		case volume.Secret != nil:
			item.Type = "Secret"
			item.Source = volume.Secret.SecretName
		case volume.ConfigMap != nil:
			item.Type = "ConfigMap"
			item.Source = volume.ConfigMap.Name
		case volume.Projected != nil:
			item.Type = "projected"
		case volume.CSI != nil:
			item.Type = "CSI"
			item.Source = volume.CSI.Driver
		case volume.DownwardAPI != nil:
			item.Type = "downwardAPI"
		}
		result = append(result, item)
	}
	return result
}
