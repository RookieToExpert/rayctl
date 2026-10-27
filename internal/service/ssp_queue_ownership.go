package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	queueUsageScopeOwnedOrUnassigned = "queue_owned_or_unassigned"
	queueNodeOwned                   = "owned"
	queueNodeUnassigned              = "unassigned"
	queueNodeOther                   = "other"
	queueNodeUnknown                 = "unknown"
	queueOwnershipACNLabel           = "resource.compute.sensecore.cn/acn-uid"
	queueOwnershipVCLabel            = "resource.compute.sensecore.cn/vc-uid"
)

type SSPQueueOwnershipSummary struct {
	Source          string `json:"source"`
	OwnedNodes      int    `json:"owned_nodes"`
	UnassignedNodes int    `json:"unassigned_nodes"`
	OtherNodes      int    `json:"other_nodes"`
	UnknownNodes    int    `json:"unknown_nodes"`
	EligibleNodes   int    `json:"eligible_nodes"`
}

// FilterQueueFreeNodes applies the queue boundary before the resource filter.
// A successful full VC snapshot is required before a node can be unassigned.
func (s *SSPResourceService) FilterQueueFreeNodes(ctx context.Context, result *SSPQueueNodeUsageResult) error {
	if result == nil {
		return nil
	}
	if !result.SharedVCPool || result.NodeUsageError != "" || result.Ownership != nil {
		result.FilterFreeNodes()
		return nil
	}
	var err error
	if normalizeQueueOwnerUID(result.Queue.UID) == "" {
		err = fmt.Errorf("队列 UID 缺失，无法可靠判定节点归属")
	} else {
		var nodes []corev1.Node
		var pods []corev1.Pod
		nodes, pods, err = s.readQueueOwnership(ctx, result.Queue)
		if err == nil {
			classifyQueueOwnership(result, nodes, pods)
		}
	}
	if err != nil {
		wrapped := &queueUsageSourceError{source: "节点归属查询", err: err}
		result.OwnershipError = wrapped.Error()
		result.FilterFreeNodes()
		return wrapped
	}
	if result.Ownership.UnknownNodes > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d 个节点归属无法确认，已排除，未计入公共池", result.Ownership.UnknownNodes))
	}
	result.FilterFreeNodes()
	return nil
}

func (s *SSPResourceService) readQueueOwnership(ctx context.Context, queue SSPQueueItem) ([]corev1.Node, []corev1.Pod, error) {
	nodePage := func(ctx context.Context, options metav1.ListOptions) (*corev1.NodeList, error) {
		return s.platform.ListSSPQueueOwnershipNodesPage(ctx, queue.Profile, queue.VCluster, options)
	}
	podPage := func(ctx context.Context, options metav1.ListOptions) (*corev1.PodList, error) {
		return s.platform.ListSSPQueueOwnershipPodsPage(ctx, queue.Profile, queue.VCluster, options)
	}
	if s.queueNodeClientResolver != nil {
		if client, _ := s.queueNodeClientResolver(queue); client != nil {
			nodePage = client.CoreV1().Nodes().List
			podPage = client.CoreV1().Pods(metav1.NamespaceAll).List
		}
	}
	nodeResult := asyncCall(ctx, func(ctx context.Context) ([]corev1.Node, error) {
		return readQueueOwnershipPages(ctx, "", func(ctx context.Context, options metav1.ListOptions) ([]corev1.Node, metav1.ListMeta, error) {
			page, err := nodePage(ctx, options)
			if err != nil {
				return nil, metav1.ListMeta{}, err
			}
			if page == nil {
				return nil, metav1.ListMeta{}, fmt.Errorf("empty node list response")
			}
			return page.Items, page.ListMeta, nil
		})
	})
	podResult := asyncCall(ctx, func(ctx context.Context) ([]corev1.Pod, error) {
		return readQueueOwnershipPages(ctx, "status.phase!=Succeeded,status.phase!=Failed", func(ctx context.Context, options metav1.ListOptions) ([]corev1.Pod, metav1.ListMeta, error) {
			page, err := podPage(ctx, options)
			if err != nil {
				return nil, metav1.ListMeta{}, err
			}
			if page == nil {
				return nil, metav1.ListMeta{}, fmt.Errorf("empty pod list response")
			}
			return page.Items, page.ListMeta, nil
		})
	})
	nodes, pods := <-nodeResult, <-podResult
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := errors.Join(nodes.Err, pods.Err); err != nil {
		return nil, nil, err
	}
	return nodes.Value, pods.Value, nil
}

func readQueueOwnershipPages[T any](ctx context.Context, selector string, fetch func(context.Context, metav1.ListOptions) ([]T, metav1.ListMeta, error)) ([]T, error) {
	options := metav1.ListOptions{Limit: 500, FieldSelector: selector}
	items := make([]T, 0)
	seen := map[string]bool{}
	version := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, meta, err := fetch(ctx, options)
		if err != nil {
			return nil, err
		}
		if version != "" && meta.ResourceVersion != "" && version != meta.ResourceVersion {
			return nil, fmt.Errorf("ownership list resource version changed during pagination")
		}
		if version == "" {
			version = meta.ResourceVersion
		}
		items = append(items, page...)
		if meta.Continue == "" {
			if meta.RemainingItemCount != nil && *meta.RemainingItemCount > 0 {
				return nil, fmt.Errorf("ownership list is incomplete without a continuation token")
			}
			return items, nil
		}
		if seen[meta.Continue] {
			return nil, fmt.Errorf("ownership list continuation token repeated")
		}
		seen[meta.Continue], options.Continue = true, meta.Continue
	}
}

type queueOwnerEvidence struct {
	owned, other, unknown bool
	uids, names           map[string]struct{}
}

func normalizeQueueOwnerUID(uid string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(uid)), "ssp-")
}

func addQueueOwner(e *queueOwnerEvidence, meta metav1.ObjectMeta, queue SSPQueueItem) bool {
	uids, names := map[string]struct{}{}, map[string]struct{}{}
	for _, fields := range []map[string]string{meta.Labels, meta.Annotations} {
		if uid := normalizeQueueOwnerUID(fields[nodeQueueUIDLabelKey]); uid != "" && uid != "-" {
			uids[uid] = struct{}{}
		}
		if name := strings.TrimSpace(fields[nodeQueueNameLabelKey]); name != "" && name != "-" {
			names[name] = struct{}{}
		}
		for _, key := range []string{"scheduling.volcano.sh/queue-name", "volcano.sh/queue-name"} {
			value := strings.TrimSpace(fields[key])
			if value == "" || value == "-" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(value), "ssp-") {
				uids[normalizeQueueOwnerUID(value)] = struct{}{}
			} else {
				names[value] = struct{}{}
			}
		}
	}
	if e.uids == nil {
		e.uids, e.names = map[string]struct{}{}, map[string]struct{}{}
	}
	for uid := range uids {
		e.uids[uid] = struct{}{}
		if uid == normalizeQueueOwnerUID(queue.UID) {
			e.owned = true
		} else {
			e.other = true
		}
	}
	for name := range names {
		e.names[name] = struct{}{}
		if len(uids) == 0 {
			if strings.EqualFold(name, queue.Name) {
				e.owned = true
			} else {
				e.other = true
			}
		}
	}
	return len(uids)+len(names) > 0
}

func classifyQueueOwnership(result *SSPQueueNodeUsageResult, nodes []corev1.Node, pods []corev1.Pod) {
	byUID, byName, byIP := map[string]*corev1.Node{}, map[string]*corev1.Node{}, map[string]*corev1.Node{}
	evidence := map[string]*queueOwnerEvidence{}
	put := func(index map[string]*corev1.Node, key string, node *corev1.Node) {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return
		}
		if _, exists := index[key]; exists {
			index[key] = nil
		} else {
			index[key] = node
		}
	}
	for i := range nodes {
		node := &nodes[i]
		put(byUID, node.Labels[queueOwnershipACNLabel], node)
		put(byName, node.Name, node)
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				put(byIP, address.Address, node)
			}
		}
		e := &queueOwnerEvidence{}
		hasOwner := addQueueOwner(e, node.ObjectMeta, result.Queue)
		if !hasOwner && strings.TrimSpace(node.Labels["resource.compute.sensecore.cn/queue-type"]) != "" {
			e.unknown = true
		}
		evidence[node.Name] = e
	}
	for _, pod := range pods {
		if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		e := evidence[pod.Spec.NodeName]
		if e == nil {
			continue
		}
		if addQueueOwner(e, pod.ObjectMeta, result.Queue) {
			continue
		}
		system := pod.Namespace == metav1.NamespaceSystem || pod.Namespace == metav1.NamespacePublic || pod.Namespace == "kube-node-lease"
		for _, container := range append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...) {
			for name, amount := range container.Resources.Requests {
				if isAcceleratorResourceName(string(name)) && amount.Sign() > 0 {
					system = false
				}
			}
		}
		if !system {
			e.unknown = true
		}
	}
	summary := &SSPQueueOwnershipSummary{Source: "vc_nodes_active_pods_and_usage"}
	for i := range result.Items {
		item := &result.Items[i]
		item.Ownership = queueNodeUnknown
		var node *corev1.Node
		ambiguous := false
		if item.acnUID != "" {
			var exists bool
			node, exists = byUID[strings.ToLower(item.acnUID)]
			ambiguous = exists && node == nil
		}
		if node == nil && !ambiguous {
			node = byName[strings.ToLower(item.HostName)]
		}
		if node == nil && !ambiguous {
			node = byIP[strings.ToLower(item.HostIP)]
		}
		valid := node != nil
		if valid && item.acnUID != "" && !strings.EqualFold(node.Labels[queueOwnershipACNLabel], item.acnUID) {
			valid = false
		}
		if valid && result.vclusterUID != "" && !strings.EqualFold(strings.TrimPrefix(node.Labels[queueOwnershipVCLabel], "vc-"), strings.TrimPrefix(result.vclusterUID, "vc-")) {
			valid = false
		}
		if valid {
			e := evidence[node.Name]
			item.OwnerQueueUIDs, item.OwnerQueueNames = sortedOwnerValues(e.uids), sortedOwnerValues(e.names)
			switch {
			case e.other:
				item.Ownership = queueNodeOther
			case e.unknown:
				item.Ownership = queueNodeUnknown
			case e.owned:
				item.Ownership = queueNodeOwned
			case zeroQueueNodeAllocation(*item):
				item.Ownership = queueNodeUnassigned
			}
		}
		switch item.Ownership {
		case queueNodeOwned:
			summary.OwnedNodes++
		case queueNodeUnassigned:
			summary.UnassignedNodes++
		case queueNodeOther:
			summary.OtherNodes++
		default:
			summary.UnknownNodes++
		}
	}
	summary.EligibleNodes = summary.OwnedNodes + summary.UnassignedNodes
	result.Ownership = summary
}

func zeroQueueNodeAllocation(item SSPQueueNodeUsageItem) bool {
	for _, amount := range []string{item.CPUAllocated, item.MemoryAllocated} {
		quantity, ok := parsePlatformResourceQuantity(amount)
		if !ok || quantity.Sign() != 0 {
			return false
		}
	}
	if item.AcceleratorTotal > 0 || item.AcceleratorAllocated != "" {
		quantity, ok := parsePlatformResourceQuantity(item.AcceleratorAllocated)
		if !ok || quantity.Sign() != 0 {
			return false
		}
	}
	return true
}

func sortedOwnerValues(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
