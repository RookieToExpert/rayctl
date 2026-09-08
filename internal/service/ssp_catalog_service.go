package service

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
	"rayctl/internal/podevidence"
)

type SSPCatalogListOptions struct {
	Region       string
	Workspace    string
	Queue        string
	State        string
	Limit        int
	All          bool
	IncludeNodes bool
}

type SSPCatalogListItem struct {
	Restarts  *int64
	UID       string
	Name      string
	State     string
	Workspace string
	Queue     string
	Node      string
	Creator   string
	Resource  string
	CreatedAt string
}

type SSPCatalogListResult struct {
	Items    []SSPCatalogListItem
	Warnings []string
	AID      bool `json:"-"`
}

type SSPCatalogService struct {
	platform  *platform.VirtualClusterClient
	clientset kubernetes.Interface
}

func NewSSPCatalogService(platformClient *platform.VirtualClusterClient) *SSPCatalogService {
	return &SSPCatalogService{platform: platformClient}
}

func (s *SSPCatalogService) SetKubeClient(clientset kubernetes.Interface) {
	s.clientset = clientset
}

func (s *SSPCatalogService) ListAIT(ctx context.Context, options SSPCatalogListOptions) (*SSPCatalogListResult, error) {
	if queue := strings.TrimSpace(options.Queue); queue != "" {
		return s.listQueueWorkloads(ctx, queue, "trainingJob", options)
	}
	return s.listWorkspaceCatalog(ctx, options, func(ctx context.Context, workspace platform.SSPWorkspace, state string, limit int) ([]SSPCatalogListItem, error) {
		jobs, err := s.platform.ListSSPTrainingJobsInWorkspace(ctx, workspace, state, limit)
		if err != nil {
			return nil, err
		}
		items := make([]SSPCatalogListItem, 0, len(jobs))
		for _, job := range jobs {
			items = append(items, SSPCatalogListItem{
				Name:      firstNonEmpty(job.Name, job.DisplayName),
				State:     job.Status.State,
				Workspace: job.WorkspaceName,
				Queue:     firstNonEmpty(job.Spec.Queue.Name, lastResourceSegment(job.Spec.Queue.ID)),
				Creator:   firstNonEmpty(job.Ownership.CreatorName, job.Ownership.CreatorID),
				Resource:  formatSSPTrainingJobResources(job),
				CreatedAt: formatSSPTime(job.Status.CreateTime),
			})
		}
		return items, nil
	})
}

func (s *SSPCatalogService) ListAID(ctx context.Context, options SSPCatalogListOptions) (*SSPCatalogListResult, error) {
	var result *SSPCatalogListResult
	var err error
	if queue := strings.TrimSpace(options.Queue); queue != "" {
		result, err = s.listQueueWorkloads(ctx, queue, "aid", options)
	} else {
		result, err = s.listWorkspaceCatalog(ctx, options, func(ctx context.Context, workspace platform.SSPWorkspace, state string, limit int) ([]SSPCatalogListItem, error) {
			aids, listErr := s.platform.ListSSPAIDsInWorkspace(ctx, workspace, state, limit)
			if listErr != nil {
				return nil, listErr
			}
			items := make([]SSPCatalogListItem, 0, len(aids))
			for _, aid := range aids {
				items = append(items, SSPCatalogListItem{
					UID:       aid.UID,
					Name:      firstNonEmpty(aid.Name, aid.DisplayName),
					State:     aid.State,
					Workspace: aid.Properties.Workload.WorkspaceName,
					Queue:     firstNonEmpty(aid.Properties.Workload.Queue.Name, lastResourceSegment(aid.Properties.Workload.Queue.ID)),
					Creator:   firstNonEmpty(aid.Properties.Ownership.CreatorName, aid.Properties.Ownership.CreatorID, aid.CreatorID),
					Resource: formatSSPAIDResourceSummary(SSPAIDResourceItem{
						CPU:         formatSSPResource(aid.Properties.Workload.BaseSpec.CPU, ""),
						Memory:      formatSSPResource(aid.Properties.Workload.BaseSpec.Memory, ""),
						Accelerator: formatSSPResource(aid.Properties.Workload.BaseSpec.AccelerateDeviceCount, ""),
						GPUModel:    aid.Properties.Workload.BaseSpec.GPUModel,
						GPUMemory:   formatSSPResource(aid.Properties.Workload.BaseSpec.GPUMemorySize, "Gi"),
						MachineType: strings.Join(aid.Properties.Workload.BaseSpec.MachineTypes, ", "),
						RDMA:        aid.Properties.Workload.BaseSpec.RDMAName,
					}),
					CreatedAt: formatSSPTime(aid.CreateTime),
				})
			}
			return items, nil
		})
	}
	if err != nil {
		return nil, err
	}
	result.AID = true
	s.enrichAIDCatalogNodes(ctx, result)
	return result, nil
}

func (s *SSPCatalogService) enrichAIDCatalogNodes(ctx context.Context, result *SSPCatalogListResult) {
	if s == nil || s.clientset == nil || result == nil || len(result.Items) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	selector := labels.Set(map[string]string{sspWorkloadTypeLabel: sspAIDWorkloadTypeValue}).AsSelector().String()
	pods, err := s.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		LabelSelector: selector, ResourceVersion: "0",
	})
	if err != nil {
		return
	}
	nodesByWorkload := make(map[string]map[string]struct{}, len(result.Items)*2)
	restartsByUID := map[string]int64{}
	restartsByName := map[string]int64{}
	for index := range pods.Items {
		pod := &pods.Items[index]
		count, _ := podevidence.RestartSummary([]corev1.Pod{*pod})
		restartsByUID[pod.Labels[sspWorkloadUIDLabel]] += count
		restartsByName[pod.Labels[sspWorkspaceNameLabel]+"/"+pod.Labels[sspWorkloadNameLabel]] += count
		node := strings.TrimSpace(pod.Spec.NodeName)
		if node == "" {
			continue
		}
		for _, key := range []string{pod.Labels[sspWorkloadUIDLabel], pod.Labels[sspWorkloadNameLabel]} {
			key = strings.ToLower(strings.TrimSpace(key))
			if key == "" {
				continue
			}
			if nodesByWorkload[key] == nil {
				nodesByWorkload[key] = make(map[string]struct{})
			}
			nodesByWorkload[key][node] = struct{}{}
		}
	}
	for index := range result.Items {
		nodeSet := nodesByWorkload[strings.ToLower(strings.TrimSpace(result.Items[index].UID))]
		item := &result.Items[index]
		count, found := restartsByUID[item.UID]
		if !found {
			count, found = restartsByName[item.Workspace+"/"+item.Name]
		}
		if found {
			item.Restarts = &count
		}
		if len(nodeSet) == 0 {
			nodeSet = nodesByWorkload[strings.ToLower(strings.TrimSpace(result.Items[index].Name))]
		}
		nodes := make([]string, 0, len(nodeSet))
		for node := range nodeSet {
			nodes = append(nodes, node)
		}
		sort.Strings(nodes)
		result.Items[index].Node = strings.Join(nodes, ", ")
	}
}

type sspCatalogWorkspaceLoader func(context.Context, platform.SSPWorkspace, string, int) ([]SSPCatalogListItem, error)

func (s *SSPCatalogService) listWorkspaceCatalog(ctx context.Context, options SSPCatalogListOptions, load sspCatalogWorkspaceLoader) (*SSPCatalogListResult, error) {
	if s.platform == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	limit, err := resolveSSPCatalogLimit(options.Limit, options.All)
	if err != nil {
		return nil, err
	}
	workspaces, err := s.platform.ListSSPWorkspaces(ctx, strings.TrimSpace(options.Region))
	if err != nil {
		return nil, fmt.Errorf("list SSP workspaces: %w", err)
	}
	if requested := strings.TrimSpace(options.Workspace); requested != "" {
		filtered := workspaces[:0]
		for _, workspace := range workspaces {
			if strings.EqualFold(strings.TrimSpace(workspace.Name), requested) || strings.EqualFold(strings.TrimSpace(workspace.UID), requested) {
				filtered = append(filtered, workspace)
			}
		}
		workspaces = filtered
		if len(workspaces) == 0 {
			return nil, fmt.Errorf("workspace %q not found", requested)
		}
	}

	type loadResult struct {
		items []SSPCatalogListItem
		err   error
	}
	loads := boundedMap(ctx, workspaces, 12, func(ctx context.Context, workspace platform.SSPWorkspace) loadResult {
		items, queryErr := load(ctx, workspace, normalizeSSPCatalogAPIState(options.State), limit)
		return loadResult{items: items, err: queryErr}
	})
	items := make([]SSPCatalogListItem, 0)
	var firstErr error
	for _, result := range loads {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		items = append(items, result.items...)
	}
	if len(items) == 0 && firstErr != nil {
		return nil, firstErr
	}
	items = filterAndSortSSPCatalogItems(items, options.State, limit)
	return &SSPCatalogListResult{Items: items}, nil
}

func normalizeSSPCatalogAPIState(state string) string {
	return strings.ToUpper(strings.TrimSpace(state))
}

func (s *SSPCatalogService) listQueueWorkloads(ctx context.Context, queueName string, workloadType string, options SSPCatalogListOptions) (*SSPCatalogListResult, error) {
	if s.platform == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	limit, err := resolveSSPCatalogLimit(options.Limit, options.All)
	if err != nil {
		return nil, err
	}
	queue, err := s.platform.FindSSPQueueResource(ctx, strings.TrimSpace(options.Region), queueName)
	if err != nil {
		return nil, fmt.Errorf("find queue %q: %w", queueName, err)
	}
	queueLimit := limit
	if queueLimit < 0 {
		queueLimit = 0
	}
	workloads, err := s.platform.ListSSPQueueWorkloads(ctx, queue.ProfileName, queue.Subscription, queue.ResourceGroup, queue.Region, queue.ClusterName, queue.Name, platform.SSPQueueWorkloadQuery{
		Type: workloadType, State: strings.TrimSpace(options.State), Limit: queueLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list workloads for queue %s: %w", queue.Name, err)
	}
	items := make([]SSPCatalogListItem, 0, len(workloads))
	for _, workload := range workloads {
		workspace := firstNonEmpty(workload.Workspace.Name, queue.WorkspaceName)
		if requested := strings.TrimSpace(options.Workspace); requested != "" && !strings.EqualFold(requested, workspace) {
			continue
		}
		items = append(items, SSPCatalogListItem{
			UID:       firstNonEmpty(workload.UID, workload.ID),
			Name:      firstNonEmpty(workload.Name, workload.DisplayName),
			State:     workload.State,
			Workspace: workspace,
			Queue:     queue.Name,
			Creator:   firstNonEmpty(workload.Ownership.CreatorName, workload.Ownership.CreatorID),
			Resource:  formatSSPWorkloadResources(workload),
			CreatedAt: formatSSPTime(workload.CreateTime),
		})
	}
	items = filterAndSortSSPCatalogItems(items, options.State, limit)
	return &SSPCatalogListResult{Items: items}, nil
}

func formatSSPTrainingJobResources(job platform.SSPTrainingJob) string {
	parts := make([]string, 0, len(job.Spec.VCJob.Tasks))
	for _, task := range job.Spec.VCJob.Tasks {
		resource := task.ResourceSpec
		values := make([]string, 0, 4)
		if cpu := compactSSPValue(resource.CPUCount); cpu != "" {
			values = append(values, cpu+"C")
		}
		if memory := compactSSPValue(resource.MemoryGiB); memory != "" {
			values = append(values, memory+"GiB")
		}
		if accelerator := compactSSPValue(resource.AccelerateDeviceCount); accelerator != "" && accelerator != "0" {
			values = append(values, accelerator+"ACC")
		}
		if len(resource.MachineTypes) > 0 {
			values = append(values, strings.Join(resource.MachineTypes, ","))
		}
		name := firstNonEmpty(task.Name, task.Role, "task")
		replicas := task.Replicas
		if replicas <= 0 {
			replicas = 1
		}
		parts = append(parts, fmt.Sprintf("%s×%d %s", name, replicas, strings.Join(values, "/")))
	}
	return strings.Join(parts, "; ")
}

func resolveSSPCatalogLimit(limit int, all bool) (int, error) {
	if all {
		return -1, nil
	}
	if limit == 0 {
		return 50, nil
	}
	if limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("--limit must be between 1 and 1000")
	}
	return limit, nil
}

func filterAndSortSSPCatalogItems(items []SSPCatalogListItem, state string, limit int) []SSPCatalogListItem {
	state = strings.TrimSpace(state)
	filtered := items[:0]
	for _, item := range items {
		if state == "" || strings.EqualFold(strings.TrimSpace(item.State), state) {
			filtered = append(filtered, item)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt == filtered[j].CreatedAt {
			return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
		}
		return filtered[i].CreatedAt > filtered[j].CreatedAt
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}
