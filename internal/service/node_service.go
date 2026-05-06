package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	repairUsageLabelKey            = "belong-usage-type"
	repairUsageLabelValue          = "repair"
	prodRoleLabelKey               = "node-role.compute.sensecore.cn/prod"
	genericRoleLabelPrefix         = "node-role.sensecore.cn/"
	nodeVClusterNamespaceLabelKey  = "cluster.x-k8s.io/vcluster-namespace"
	nsVClusterNamespaceLabelKey    = "vcluster.loft.sh/vcluster-namespace"
	metaxGPUResourceName           = "metax-tech.com/gpu"
	huaweiGPUResourceName          = "huawei.com/Ascend910"
	metaxGPUTopologyAnnotationKey  = "metax-tech.com/gpu.topology.zones"
)

var nodeSelectorProfiles = map[string]string{
	// Replace these selectors with the real labels used in your cluster.
	"ecp": "node-role.compute.sensecore.cn/prod=ecp-private",
	"ecs": "node-role.compute.sensecore.cn/prod=ecs",
}

type NodeService struct {
	clientset kubernetes.Interface
}

type NodeListItem struct {
	Name        string
	Ready       string
	Schedulable string
	UsageType   string
	Repair      bool
	ProdRole    string
	InternalIP  string
	ClusterName string
	ClusterUID  string
}

type NodeMutationResult struct {
	Name        string
	Action      string
	Schedulable string
	RepairLabel string
}

type NodeDescribe struct {
	Hostname         string
	VClusterName     string
	VClusterUID      string
	Unschedulable    bool
	Repair           bool
	GPUUsage         string
	CPUUsage         string
	MemoryUsage      string
	Pods             []string
	MatchedPodCount  int
	Timings          DescribeTimings
}

type DescribeTimings struct {
	GetNode        time.Duration
	ListNamespaces time.Duration
	ListPods       time.Duration
	Summarize      time.Duration
	Total          time.Duration
}

func NewNodeService(clientset kubernetes.Interface) *NodeService {
	return &NodeService{clientset: clientset}
}

func (s *NodeService) List(ctx context.Context, target string, extraSelector string) ([]NodeListItem, string, error) {
	resolvedSelector, err := resolveNodeSelector(target, extraSelector)
	if err != nil {
		return nil, "", err
	}

	nodes, err := s.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: resolvedSelector,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list nodes: %w", err)
	}

	result := make([]NodeListItem, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		result = append(result, NodeListItem{
			Name:        node.Name,
			Ready:       nodeReadyStatus(node.Status.Conditions),
			Schedulable: nodeSchedulable(node.Spec.Unschedulable),
			UsageType:   node.Labels[repairUsageLabelKey],
			Repair:      node.Spec.Unschedulable,
			ProdRole:    nodeDisplayRole(node.Labels),
			InternalIP:  nodeInternalIP(node.Status.Addresses),
			ClusterName: firstNonEmpty(
				node.Labels["cluster.x-k8s.io/vcluster-name"],
				node.Labels["cluster.x-k8s.io/cluster-name"],
				node.Labels[nodeVClusterNamespaceLabelKey],
				node.Labels["resource.compute.sensecore.cn/vc-uid"],
			),
			ClusterUID: node.Labels["resource.compute.sensecore.cn/vc-uid"],
		})
	}

	return result, resolvedSelector, nil
}

func nodeDisplayRole(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	if role := strings.TrimSpace(labels[prodRoleLabelKey]); role != "" {
		return role
	}

	roles := make([]string, 0, 2)
	for key, value := range labels {
		if !strings.HasPrefix(key, genericRoleLabelPrefix) {
			continue
		}
		roleName := strings.TrimPrefix(key, genericRoleLabelPrefix)
		roleName = strings.TrimSpace(roleName)
		if roleName == "" {
			roleName = strings.TrimSpace(value)
		}
		if roleName == "" {
			continue
		}
		roles = append(roles, roleName)
	}

	sort.Strings(roles)
	if len(roles) == 0 {
		return ""
	}
	return strings.Join(roles, ",")
}

func (s *NodeService) Describe(ctx context.Context, nodeName string) (*NodeDescribe, error) {
	startedAt := time.Now()
	getNodeBegin := time.Now()
	node, err := s.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	getNodeDuration := time.Since(getNodeBegin)
	if err != nil {
		return nil, fmt.Errorf("get node %q: %w", nodeName, err)
	}

	listNamespacesBegin := time.Now()
	targetNamespaces, err := s.listTargetNamespacesForNode(ctx, node)
	listNamespacesDuration := time.Since(listNamespacesBegin)
	if err != nil {
		return nil, err
	}

	listPodsBegin := time.Now()
	pods, err := s.listPodsOnNodeInNamespaces(ctx, nodeName, targetNamespaces)
	listPodsDuration := time.Since(listPodsBegin)
	if err != nil {
		return nil, err
	}

	summarizeBegin := time.Now()
	allocatedCPU, allocatedMemory, allocatedGPU, podNames := summarizePodRequests(pods)
	totalCPU, totalMemory, totalGPU := nodeCapacitySummary(node.Status.Allocatable)
	if directAllocatedGPU, ok := directNodeGPUAllocated(node); ok {
		allocatedGPU = directAllocatedGPU
	}
	summarizeDuration := time.Since(summarizeBegin)

	return &NodeDescribe{
		Hostname:        node.Name,
		VClusterName:    firstNonEmpty(
			node.Labels["cluster.x-k8s.io/vcluster-name"],
			node.Labels["cluster.x-k8s.io/cluster-name"],
			node.Labels[nodeVClusterNamespaceLabelKey],
			node.Labels["resource.compute.sensecore.cn/vc-uid"],
		),
		VClusterUID:     strings.TrimSpace(node.Labels["resource.compute.sensecore.cn/vc-uid"]),
		Unschedulable:   node.Spec.Unschedulable,
		Repair:          node.Labels[repairUsageLabelKey] == repairUsageLabelValue,
		GPUUsage:        formatGPUUsage(allocatedGPU, totalGPU),
		CPUUsage:        formatCPUUsage(allocatedCPU, totalCPU),
		MemoryUsage:     formatMemoryUsage(allocatedMemory, totalMemory),
		Pods:            podNames,
		MatchedPodCount: len(podNames),
		Timings: DescribeTimings{
			GetNode:        getNodeDuration,
			ListNamespaces: listNamespacesDuration,
			ListPods:       listPodsDuration,
			Summarize:      summarizeDuration,
			Total:          time.Since(startedAt),
		},
	}, nil
}

func (s *NodeService) Cordon(ctx context.Context, nodeName string) (*NodeMutationResult, error) {
	patch := map[string]any{
		"spec": map[string]any{
			"unschedulable": true,
		},
		"metadata": map[string]any{
			"labels": map[string]any{
				repairUsageLabelKey: repairUsageLabelValue,
			},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("marshal cordon patch for node %q: %w", nodeName, err)
	}

	updatedNode, err := s.clientset.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("cordon node %q: %w", nodeName, err)
	}

	return &NodeMutationResult{
		Name:        updatedNode.Name,
		Action:      "cordoned",
		Schedulable: nodeSchedulable(updatedNode.Spec.Unschedulable),
		RepairLabel: updatedNode.Labels[repairUsageLabelKey],
	}, nil
}

func (s *NodeService) Uncordon(ctx context.Context, nodeName string) (*NodeMutationResult, error) {
	patch := map[string]any{
		"spec": map[string]any{
			"unschedulable": false,
		},
		"metadata": map[string]any{
			"labels": map[string]any{
				repairUsageLabelKey: nil,
			},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("marshal uncordon patch for node %q: %w", nodeName, err)
	}

	updatedNode, err := s.clientset.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("uncordon node %q: %w", nodeName, err)
	}

	repairLabel := ""
	if updatedNode.Labels != nil {
		repairLabel = updatedNode.Labels[repairUsageLabelKey]
	}

	return &NodeMutationResult{
		Name:        updatedNode.Name,
		Action:      "uncordoned",
		Schedulable: nodeSchedulable(updatedNode.Spec.Unschedulable),
		RepairLabel: repairLabel,
	}, nil
}

func (s *NodeService) listTargetNamespacesForNode(ctx context.Context, node *corev1.Node) ([]string, error) {
	vclusterNamespace := node.Labels[nodeVClusterNamespaceLabelKey]
	if vclusterNamespace == "" {
		return nil, fmt.Errorf("node %q is missing label %q", node.Name, nodeVClusterNamespaceLabelKey)
	}

	namespaces, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", nsVClusterNamespaceLabelKey, vclusterNamespace),
	})
	if err != nil {
		return nil, fmt.Errorf("list namespaces for node %q via %q=%q: %w", node.Name, nsVClusterNamespaceLabelKey, vclusterNamespace, err)
	}

	items := make([]string, 0, len(namespaces.Items))
	for _, namespace := range namespaces.Items {
		items = append(items, namespace.Name)
	}

	sort.Strings(items)
	return items, nil
}

func (s *NodeService) listPodsOnNodeInNamespaces(ctx context.Context, nodeName string, namespaces []string) ([]corev1.Pod, error) {
	if len(namespaces) == 0 {
		return nil, nil
	}

	fieldSelector := fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	type namespaceResult struct {
		pods []corev1.Pod
		err  error
	}

	results := make(chan namespaceResult, len(namespaces))
	var wg sync.WaitGroup

	for _, namespace := range namespaces {
		namespace := namespace
		wg.Add(1)
		go func() {
			defer wg.Done()
			pods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				FieldSelector: fieldSelector,
			})
			if err != nil {
				results <- namespaceResult{
					err: fmt.Errorf("list pods on node %q in namespace %q: %w", nodeName, namespace, err),
				}
				return
			}

			filtered := make([]corev1.Pod, 0, len(pods.Items))
			for _, pod := range pods.Items {
				if pod.Status.Phase != corev1.PodRunning {
					continue
				}
				filtered = append(filtered, pod)
			}

			results <- namespaceResult{pods: filtered}
		}()
	}

	wg.Wait()
	close(results)

	result := make([]corev1.Pod, 0)
	for item := range results {
		if item.err != nil {
			return nil, item.err
		}
		result = append(result, item.pods...)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Namespace == result[j].Namespace {
			return result[i].Name < result[j].Name
		}
		return result[i].Namespace < result[j].Namespace
	})

	return result, nil
}

func summarizePodRequests(pods []corev1.Pod) (resource.Quantity, resource.Quantity, int64, []string) {
	cpuTotal := *resource.NewMilliQuantity(0, resource.DecimalSI)
	memoryTotal := *resource.NewQuantity(0, resource.BinarySI)
	var gpuTotal int64
	podNames := make([]string, 0, len(pods))

	for _, pod := range pods {
		podNames = append(podNames, pod.Name)

		for _, container := range pod.Spec.Containers {
			if cpu := container.Resources.Requests.Cpu(); cpu != nil {
				cpuTotal.Add(*cpu)
			}
			if memory := container.Resources.Requests.Memory(); memory != nil {
				memoryTotal.Add(*memory)
			}
			gpuTotal += gpuRequestValue(container.Resources.Requests)
		}
	}

	return cpuTotal, memoryTotal, gpuTotal, podNames
}

func nodeCapacitySummary(resources corev1.ResourceList) (resource.Quantity, resource.Quantity, int64) {
	cpuTotal := *resource.NewMilliQuantity(0, resource.DecimalSI)
	memoryTotal := *resource.NewQuantity(0, resource.BinarySI)
	var gpuTotal int64

	if cpu := resources.Cpu(); cpu != nil {
		cpuTotal = cpu.DeepCopy()
	}
	if memory := resources.Memory(); memory != nil {
		memoryTotal = memory.DeepCopy()
	}
	gpuTotal = gpuCapacityValue(resources)

	return cpuTotal, memoryTotal, gpuTotal
}

func gpuRequestValue(resources corev1.ResourceList) int64 {
	for _, resourceName := range gpuResourceNames() {
		if gpu, ok := resources[corev1.ResourceName(resourceName)]; ok {
			return gpu.Value()
		}
	}
	return 0
}

func gpuCapacityValue(resources corev1.ResourceList) int64 {
	for _, resourceName := range gpuResourceNames() {
		if gpu, ok := resources[corev1.ResourceName(resourceName)]; ok {
			return gpu.Value()
		}
	}
	return 0
}

func gpuResourceNames() []string {
	return []string{
		metaxGPUResourceName,
		huaweiGPUResourceName,
	}
}

func directNodeGPUAllocated(node *corev1.Node) (int64, bool) {
	annotation := node.Annotations[metaxGPUTopologyAnnotationKey]
	if annotation == "" {
		return 0, false
	}

	var payload struct {
		Zones []struct {
			Resources []struct {
				Name        string `json:"name"`
				Allocatable string `json:"allocatable"`
				Available   string `json:"available"`
			} `json:"resources"`
		} `json:"zones"`
	}

	if err := json.Unmarshal([]byte(annotation), &payload); err != nil {
		return 0, false
	}

	for _, zone := range payload.Zones {
		for _, resourceItem := range zone.Resources {
			if resourceItem.Name != metaxGPUResourceName {
				continue
			}

			allocatable, err := resource.ParseQuantity(resourceItem.Allocatable)
			if err != nil {
				return 0, false
			}
			available, err := resource.ParseQuantity(resourceItem.Available)
			if err != nil {
				return 0, false
			}

			return allocatable.Value() - available.Value(), true
		}
	}

	return 0, false
}

func resolveNodeSelector(target string, extraSelector string) (string, error) {
	selectors := make([]string, 0, 2)

	if target != "" {
		if selector, ok := nodeSelectorProfiles[strings.ToLower(target)]; ok {
			selectors = append(selectors, selector)
		} else {
			selectors = append(selectors, target)
		}
	}

	if extraSelector != "" {
		selectors = append(selectors, extraSelector)
	}

	return strings.Join(selectors, ","), nil
}

func formatGPUUsage(allocated, total int64) string {
	return fmt.Sprintf("%d/%d", allocated, total)
}

func formatCPUUsage(allocated, total resource.Quantity) string {
	return fmt.Sprintf("%s/%s", humanCPU(allocated), humanCPU(total))
}

func formatMemoryUsage(allocated, total resource.Quantity) string {
	return fmt.Sprintf("%s/%s", humanMemory(allocated), humanMemory(total))
}

func humanCPU(q resource.Quantity) string {
	milli := q.MilliValue()
	if milli%1000 == 0 {
		return fmt.Sprintf("%d", milli/1000)
	}
	return fmt.Sprintf("%.3f", float64(milli)/1000.0)
}

func humanMemory(q resource.Quantity) string {
	bytes := q.Value()
	const gi = int64(1024 * 1024 * 1024)
	const mi = int64(1024 * 1024)

	if bytes%gi == 0 {
		return fmt.Sprintf("%dGi", bytes/gi)
	}
	return fmt.Sprintf("%dMi", bytes/mi)
}

func nodeReadyStatus(conditions []corev1.NodeCondition) string {
	for _, condition := range conditions {
		if condition.Type == corev1.NodeReady {
			if condition.Status == corev1.ConditionTrue {
				return "Ready"
			}
			return "NotReady"
		}
	}

	return "Unknown"
}

func nodeSchedulable(unschedulable bool) string {
	if unschedulable {
		return "No"
	}
	return "Yes"
}

func nodeInternalIP(addresses []corev1.NodeAddress) string {
	for _, address := range addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address
		}
	}
	return "-"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
