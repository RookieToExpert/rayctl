package service

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"rayctl/internal/platform"
)

type NodeWorkload struct {
	Namespace           string
	Pod                 string
	VC                  string
	Type                string
	Workload            string
	WorkloadUID         string
	Workspace           string
	Queue               string
	Creator             string
	Phase               string
	Status              string
	AcceleratorRequests map[string]string
	Restarts            int64
	RuntimeSeconds      int64
	Source              string
}

// WithWorkloads must be configured before starting concurrent Describe calls.
func (s *NodeService) WithWorkloads() *NodeService {
	s.includeWorkloads = true
	return s
}

func (s *NodeService) getNodeByNameOrIP(ctx context.Context, identifier string) (*corev1.Node, error) {
	ip := net.ParseIP(identifier)
	if ip == nil {
		return s.clientset.CoreV1().Nodes().Get(ctx, identifier, metav1.GetOptions{})
	}
	nodes, err := s.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var found *corev1.Node
	for i := range nodes.Items {
		for _, address := range nodes.Items[i].Status.Addresses {
			if (address.Type == corev1.NodeInternalIP || address.Type == corev1.NodeExternalIP) && ip.Equal(net.ParseIP(address.Address)) {
				if found != nil && found.Name != nodes.Items[i].Name {
					return nil, fmt.Errorf("multiple nodes have IP %s", identifier)
				}
				found = &nodes.Items[i]
			}
		}
	}
	if found == nil {
		return nil, fmt.Errorf("node IP %q not found", identifier)
	}
	return found, nil
}

func (s *NodeService) listWorkloadPodsOnNode(ctx context.Context, node string) ([]corev1.Pod, error) {
	options := metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node).String(), Limit: 500}
	var result []corev1.Pod
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pods, err := s.clientset.CoreV1().Pods("").List(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("list workloads on node %q: %w", node, err)
		}
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == node {
				result = append(result, pod)
			}
		}
		if pods.Continue == "" {
			break
		}
		if seen[pods.Continue] {
			return nil, fmt.Errorf("node pod pagination token repeated")
		}
		seen[pods.Continue], options.Continue = true, pods.Continue
	}
	return result, nil
}

func nodeWorkloadsFromPods(pods []corev1.Pod) []NodeWorkload {
	result := make([]NodeWorkload, 0, len(pods))
	for _, pod := range pods {
		identity := podLocateResult(pod)
		kind := identity.WorkloadType
		switch kind {
		case "AIT", "ATE", "AID", "AIR", "ECS":
		default:
			kind = "OTHER"
		}
		name, uid := identity.WorkloadName, identity.WorkloadUID
		if name == "" {
			for _, owner := range pod.OwnerReferences {
				if owner.Controller != nil && *owner.Controller {
					name, uid = owner.Name, string(owner.UID)
					break
				}
			}
		}
		item := NodeWorkload{Namespace: pod.Namespace, Pod: pod.Name, VC: identity.VC, Type: kind, Workload: name, WorkloadUID: uid,
			Workspace: identity.Workspace, Queue: identity.Queue, Creator: identity.Creator, Phase: identity.Phase, Status: nodePodStatus(pod),
			Restarts: identity.Restarts, Source: "k8s", AcceleratorRequests: map[string]string{}}
		for _, container := range append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...) {
			for resourceName := range container.Resources.Requests {
				if isAcceleratorResourceName(string(resourceName)) {
					quantity := podNamedResourceRequest(pod.Spec, resourceName)
					item.AcceleratorRequests[string(resourceName)] = quantity.String()
				}
			}
		}
		if pod.Status.StartTime != nil {
			end := time.Now()
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				end = pod.Status.StartTime.Time
				for _, status := range pod.Status.ContainerStatuses {
					if status.State.Terminated != nil && status.State.Terminated.FinishedAt.After(end) {
						end = status.State.Terminated.FinishedAt.Time
					}
				}
			}
			item.RuntimeSeconds = max(0, int64(end.Sub(pod.Status.StartTime.Time).Seconds()))
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].Pod < result[j].Pod
	})
	return result
}

// nodePodStatus follows the status reason shown by kubectl get pod. Pod phase
// alone hides container states such as ContainerCreating and CrashLoopBackOff.
func nodePodStatus(pod corev1.Pod) string {
	reason := string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
	}
	initializing := false
	for i, status := range pod.Status.InitContainerStatuses {
		if i < len(pod.Spec.InitContainers) &&
			pod.Spec.InitContainers[i].RestartPolicy != nil &&
			*pod.Spec.InitContainers[i].RestartPolicy == corev1.ContainerRestartPolicyAlways &&
			status.Started != nil && *status.Started {
			continue
		}
		if status.State.Terminated != nil && status.State.Terminated.ExitCode == 0 {
			continue
		}
		initializing = true
		switch {
		case status.State.Terminated != nil:
			reason = status.State.Terminated.Reason
			if reason == "" {
				reason = fmt.Sprintf("ExitCode:%d", status.State.Terminated.ExitCode)
			}
			reason = "Init:" + reason
		case status.State.Waiting != nil && status.State.Waiting.Reason != "":
			reason = "Init:" + status.State.Waiting.Reason
		default:
			reason = fmt.Sprintf("Init:%d/%d", i, len(pod.Spec.InitContainers))
		}
		break
	}
	if !initializing && len(pod.Status.InitContainerStatuses) < len(pod.Spec.InitContainers) {
		initializing = true
		reason = fmt.Sprintf("Init:%d/%d", len(pod.Status.InitContainerStatuses), len(pod.Spec.InitContainers))
	}
	if !initializing {
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			status := pod.Status.ContainerStatuses[i]
			switch {
			case status.State.Waiting != nil && status.State.Waiting.Reason != "":
				reason = status.State.Waiting.Reason
			case status.State.Terminated != nil:
				reason = status.State.Terminated.Reason
				if reason == "" {
					reason = fmt.Sprintf("ExitCode:%d", status.State.Terminated.ExitCode)
				}
			}
		}
	}
	if pod.DeletionTimestamp != nil {
		if pod.Status.Reason == "NodeLost" {
			return "Unknown"
		}
		return "Terminating"
	}
	return reason
}

type NodeWorkloadPlatform interface {
	CurrentProfileName() string
	GetSSPQueueResource(context.Context, string, string) (*platform.SSPQueueResourceDetails, error)
	ResolveUsernames(context.Context, []string) (map[string]string, error)
	ResolveDisplayNamesWithProfiles(context.Context, []string) (map[string]string, map[string]string, error)
}

// EnrichNodeWorkloads shares one deadline across all nodes and deduplicated lookups.
func EnrichNodeWorkloads(ctx context.Context, results []NodeDescribeQueryResult, client NodeWorkloadPlatform) {
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	queues, users, vcs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, result := range results {
		if result.Err != nil || result.Details == nil {
			continue
		}
		for _, row := range result.Details.Workloads {
			if row.Queue != "" && (row.Workspace == "" || row.VC == "" || looksLikePodVCUID(row.VC)) {
				queues[row.Queue] = true
			}
			if looksLikeUUID(row.Creator) {
				users[row.Creator] = true
			}
			if looksLikePodVCUID(row.VC) {
				vcs[strings.TrimPrefix(row.VC, "vc-")] = true
			}
		}
	}
	keys := func(values map[string]bool) []string {
		result := make([]string, 0, len(values))
		for key := range values {
			result = append(result, key)
		}
		sort.Strings(result)
		return result
	}
	type lookup struct{ kind, key string }
	type resolved struct {
		lookup
		queue *platform.SSPQueueResourceDetails
		names map[string]string
		err   error
	}
	queries := []lookup{}
	if len(users) > 0 {
		queries = append(queries, lookup{kind: "users"})
	}
	if len(vcs) > 0 {
		queries = append(queries, lookup{kind: "vcs"})
	}
	for _, queue := range keys(queues) {
		queries = append(queries, lookup{kind: "queue", key: queue})
	}
	values := boundedMap(ctx, queries, 4, func(ctx context.Context, query lookup) resolved {
		r := resolved{lookup: query}
		if r.err = ctx.Err(); r.err != nil {
			return r
		}
		switch query.kind {
		case "users":
			r.names, r.err = client.ResolveUsernames(ctx, keys(users))
		case "vcs":
			r.names, _, r.err = client.ResolveDisplayNamesWithProfiles(ctx, keys(vcs))
		case "queue":
			r.queue, r.err = client.GetSSPQueueResource(ctx, client.CurrentProfileName(), query.key)
		}
		return r
	})
	for _, result := range results {
		if result.Err != nil || result.Details == nil {
			continue
		}
		details := result.Details
		for _, value := range values {
			if value.err != nil {
				details.Warnings = append(details.Warnings, fmt.Sprintf("workload %s %s: %v", value.kind, value.key, value.err))
				continue
			}
			for i := range details.Workloads {
				row := &details.Workloads[i]
				before := *row
				switch value.kind {
				case "users":
					if name := value.names[row.Creator]; name != "" {
						row.Creator = name
					}
				case "vcs":
					if name := value.names[strings.TrimPrefix(row.VC, "vc-")]; name != "" {
						row.VC = name
					}
				case "queue":
					if row.Queue == value.key && value.queue != nil {
						row.Workspace = firstNonEmpty(row.Workspace, value.queue.WorkspaceName)
						row.VC = firstNonEmpty(value.queue.VClusterName, row.VC)
					}
				}
				if before.Creator != row.Creator || before.VC != row.VC || before.Workspace != row.Workspace {
					row.Source = "platform"
				}
			}
		}
	}
}
