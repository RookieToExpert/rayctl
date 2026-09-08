package service

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type nodeRDMAUsage struct {
	allocated resource.Quantity
	total     resource.Quantity
}

func kubernetesNodeRDMAUsage(nodes []corev1.Node, pods []corev1.Pod) map[string]nodeRDMAUsage {
	allocatedByNode := make(map[string]resource.Quantity)
	for index := range pods {
		pod := &pods[index]
		nodeName := normalizeVCNodeLookupKey(pod.Spec.NodeName)
		if nodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		quantity := podRDMARequest(pod.Spec)
		if quantity.IsZero() {
			continue
		}
		current := allocatedByNode[nodeName]
		current.Add(quantity)
		allocatedByNode[nodeName] = current
	}

	result := make(map[string]nodeRDMAUsage, len(nodes)*2)
	for index := range nodes {
		node := &nodes[index]
		total := rdmaQuantity(node.Status.Allocatable)
		allocated := allocatedByNode[normalizeVCNodeLookupKey(node.Name)]
		if total.IsZero() && allocated.IsZero() {
			continue
		}
		usage := nodeRDMAUsage{allocated: allocated, total: total}
		if name := normalizeVCNodeLookupKey(node.Name); name != "" {
			result[name] = usage
		}
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				result[normalizeVCNodeLookupKey(address.Address)] = usage
			}
		}
	}
	return result
}

func podRDMARequest(spec corev1.PodSpec) resource.Quantity {
	regular := resource.MustParse("0")
	for index := range spec.Containers {
		regular.Add(rdmaQuantity(spec.Containers[index].Resources.Requests))
	}
	initMaximum := resource.MustParse("0")
	for index := range spec.InitContainers {
		quantity := rdmaQuantity(spec.InitContainers[index].Resources.Requests)
		if quantity.Cmp(initMaximum) > 0 {
			initMaximum = quantity
		}
	}
	if initMaximum.Cmp(regular) > 0 {
		regular = initMaximum
	}
	regular.Add(rdmaQuantity(spec.Overhead))
	return regular
}

func rdmaQuantity(resources corev1.ResourceList) resource.Quantity {
	total := resource.MustParse("0")
	for name, quantity := range resources {
		if isRDMAResourceName(string(name)) {
			total.Add(quantity)
		}
	}
	return total
}

func rdmaUsageStrings(usage nodeRDMAUsage) (string, string) {
	return usage.allocated.String(), usage.total.String()
}

func lookupNodeRDMAUsage(usages map[string]nodeRDMAUsage, hostName string, hostIP string) (nodeRDMAUsage, bool) {
	for _, value := range []string{hostName, hostIP} {
		if usage, ok := usages[normalizeVCNodeLookupKey(strings.TrimSpace(value))]; ok {
			return usage, true
		}
	}
	return nodeRDMAUsage{}, false
}
