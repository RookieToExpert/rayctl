package service

import corev1 "k8s.io/api/core/v1"

// NodePodDetail preserves the Kubernetes Pod alongside its business identity.
type NodePodDetail struct {
	Namespace   string
	Pod         string
	Type        string
	Workload    string
	WorkloadUID string
}

func nodePodDetailsFromPods(pods []corev1.Pod) []NodePodDetail {
	items := make([]NodePodDetail, 0, len(pods))
	for _, pod := range pods {
		identity := podLocateResult(pod)
		item := NodePodDetail{Namespace: pod.Namespace, Pod: pod.Name}
		switch identity.WorkloadType {
		case "AIT", "ATE", "AID", "AIR", "ECS":
			item.Type = identity.WorkloadType
			item.Workload = identity.WorkloadName
			item.WorkloadUID = identity.WorkloadUID
		}
		items = append(items, item)
	}
	return items
}
