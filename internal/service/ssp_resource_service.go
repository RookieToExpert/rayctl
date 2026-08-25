package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/platform"
)

const (
	sspQueueUIDLabel          = "resource.compute.sensecore.cn/queue-uid"
	sspWorkspaceQueueUIDLabel = "resource.compute.sensecore.cn/workspace-queue-uid"
)

type SSPResourceService struct {
	clientset               kubernetes.Interface
	platform                *platform.VirtualClusterClient
	sspBase                 *SSPJobService
	queueNodeClientResolver func(SSPQueueItem) (kubernetes.Interface, error)
}

type SSPClusterResourceItem struct {
	ResourceType string
	Unit         string
	Allocated    string
	Total        string
	Unallocated  string
	Spot         string
	Elastic      string
}

type SSPClusterItem struct {
	Name           string
	UID            string
	State          string
	Type           string
	VCluster       string
	VClusterUID    string
	VPCUID         string
	InfraType      string
	QueueCount     int
	NodeCount      int
	ReadyNodes     int
	IdleNodes      int
	UnhealthyNodes int
	Subscription   string
	ResourceGroup  string
	Region         string
	Profile        string
	CreatedAt      string
	UpdatedAt      string
	Resources      []SSPClusterResourceItem
	Queues         []SSPQueueItem
}

type SSPClusterListResult struct {
	Items []SSPClusterItem
}

type SSPWorkspaceItem struct {
	Name          string
	UID           string
	State         string
	VCluster      string
	Subscription  string
	ResourceGroup string
	Region        string
	Profile       string
	CreatedAt     string
	UpdatedAt     string
	Queues        []SSPQueueItem
}

type SSPWorkspaceListResult struct {
	Items []SSPWorkspaceItem
}

type SSPQueueItem struct {
	Name          string
	UID           string
	State         string
	Type          string
	Workspace     string
	WorkspaceUID  string
	Cluster       string
	ClusterUID    string
	VCluster      string
	Subscription  string
	ResourceGroup string
	Region        string
	Profile       string
	CreatedAt     string
	UpdatedAt     string
	NodeCount     int
	SpotLending   string
	DequeuePolicy string
	Timings       SSPQueueGetTimings
}

type SSPQueueGetTimings struct {
	ResourceLookup time.Duration
	DetailLookup   time.Duration
	DetailReason   string
	FallbackLookup time.Duration
	Total          time.Duration
}

type SSPQueueListResult struct {
	Items []SSPQueueItem
}

type SSPQueueWorkloadQuery struct {
	Type     string
	State    string
	Priority string
}

type SSPQueueWorkloadItem struct {
	Queue     string
	Type      string
	Name      string
	UID       string
	State     string
	Workspace string
	Priority  string
	Resources string
	Creator   string
	CreatorID string
	CreatedAt string
}

type SSPQueueWorkloadResult struct {
	Queue SSPQueueItem
	Items []SSPQueueWorkloadItem
}

type SSPQueueNodeListResult struct {
	Queue SSPQueueItem
	Items []VCNodeListItem
}

type SSPQueueNodeUsageItem struct {
	HostName             string
	HostIP               string
	State                string
	CPU                  string
	Memory               string
	Accelerator          string
	CPUAllocated         string
	CPUTotal             string
	MemoryAllocated      string
	MemoryTotal          string
	AcceleratorAllocated string
	AcceleratorTotalText string
	AcceleratorFree      int64
	AcceleratorTotal     int64
}

type SSPQueueNodeUsageResult struct {
	Queue SSPQueueItem
	Items []SSPQueueNodeUsageItem
}

func (r *SSPQueueNodeUsageResult) FilterFreeAcceleratorNodes() {
	if r == nil {
		return
	}
	filtered := r.Items[:0]
	for _, item := range r.Items {
		if item.AcceleratorFree > 0 {
			filtered = append(filtered, item)
		}
	}
	r.Items = filtered
}

func NewSSPResourceService(clientset kubernetes.Interface, platformClient *platform.VirtualClusterClient) *SSPResourceService {
	return &SSPResourceService{
		clientset: clientset,
		platform:  platformClient,
		sspBase:   NewSSPJobService(clientset, platformClient),
	}
}

func (s *SSPResourceService) SetQueueNodeClientResolver(resolver func(SSPQueueItem) (kubernetes.Interface, error)) {
	s.queueNodeClientResolver = resolver
}

func (s *SSPResourceService) ListClusters(ctx context.Context, region string) (*SSPClusterListResult, error) {
	clusters, err := s.platform.ListSSPClusters(ctx, s.resolveRegion(region))
	if err != nil {
		return nil, fmt.Errorf("list SSP clusters: %w", err)
	}
	items := make([]SSPClusterItem, 0, len(clusters))
	for _, cluster := range clusters {
		items = append(items, sspClusterItem(cluster))
	}
	return &SSPClusterListResult{Items: items}, nil
}

func (s *SSPResourceService) GetCluster(ctx context.Context, identifier string, region string) (*SSPClusterItem, error) {
	clusters, err := s.platform.ListSSPClusters(ctx, s.resolveRegion(region))
	if err != nil {
		return nil, fmt.Errorf("list SSP clusters: %w", err)
	}
	cluster, err := matchSSPCluster(identifier, clusters)
	if err != nil {
		return nil, err
	}

	var detail *platform.SSPCluster
	var resources []platform.SSPResourceSummary
	var queues []platform.SSPQueue
	var detailErr, resourcesErr, queuesErr error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		detail, detailErr = s.platform.GetSSPCluster(ctx, cluster)
	}()
	go func() {
		defer wg.Done()
		resources, resourcesErr = s.platform.GetSSPClusterSummary(ctx, cluster)
	}()
	go func() {
		defer wg.Done()
		queues, queuesErr = s.platform.ListSSPClusterQueues(ctx, cluster)
	}()
	wg.Wait()
	if detailErr != nil {
		return nil, fmt.Errorf("get SSP cluster %s: %w", cluster.Name, detailErr)
	}
	if resourcesErr != nil {
		return nil, fmt.Errorf("get SSP cluster %s summary: %w", cluster.Name, resourcesErr)
	}
	if queuesErr != nil {
		return nil, fmt.Errorf("list SSP cluster %s queues: %w", cluster.Name, queuesErr)
	}

	item := sspClusterItem(*detail)
	item.Resources = make([]SSPClusterResourceItem, 0, len(resources))
	for _, resource := range resources {
		item.Resources = append(item.Resources, SSPClusterResourceItem{
			ResourceType: resource.ResourceType,
			Unit:         resource.Unit,
			Allocated:    resource.Allocated,
			Total:        resource.Total,
			Unallocated:  resource.Unallocated,
			Spot:         resource.SpotQueueAllocated,
			Elastic:      resource.ElasticQueueAllocated,
		})
	}
	sort.SliceStable(item.Resources, func(i, j int) bool {
		return sspClusterResourceOrder(item.Resources[i].ResourceType) < sspClusterResourceOrder(item.Resources[j].ResourceType)
	})
	item.Queues = make([]SSPQueueItem, 0, len(queues))
	for _, queue := range queues {
		item.Queues = append(item.Queues, SSPQueueItem{
			Name:          firstNonEmpty(queue.Name, queue.DisplayName),
			UID:           queue.UID,
			State:         queue.State,
			Type:          firstNonEmpty(queue.Type, queue.QueueType, queue.Properties.Type, queue.Properties.QueueType),
			Workspace:     firstNonEmpty(queue.WorkspaceName, queue.Properties.Workspace.Name),
			WorkspaceUID:  queue.Properties.Workspace.UID,
			Cluster:       item.Name,
			ClusterUID:    item.UID,
			VCluster:      item.VCluster,
			Subscription:  item.Subscription,
			ResourceGroup: item.ResourceGroup,
			Region:        item.Region,
			Profile:       item.Profile,
			CreatedAt:     formatSSPTime(queue.CreateTime),
			UpdatedAt:     formatSSPTime(queue.UpdateTime),
			NodeCount:     queue.Properties.NodeStatus.Total,
		})
	}
	sortSSPQueueItems(item.Queues)
	return &item, nil
}

func sspClusterResourceOrder(resourceType string) int {
	switch strings.ToUpper(strings.TrimSpace(resourceType)) {
	case "CPU":
		return 0
	case "MEMORY":
		return 1
	case "DEVICE":
		return 2
	case "NODE":
		return 3
	default:
		return 4
	}
}

func sspClusterItem(cluster platform.SSPCluster) SSPClusterItem {
	return SSPClusterItem{
		Name:           firstNonEmpty(cluster.Name, cluster.DisplayName),
		UID:            cluster.UID,
		State:          cluster.State,
		Type:           cluster.Properties.Type,
		VCluster:       cluster.Properties.Source.Name,
		VClusterUID:    cluster.Properties.Source.UID,
		VPCUID:         cluster.Properties.VPCUID,
		InfraType:      cluster.Properties.InfraType,
		QueueCount:     cluster.Properties.QueueStatus.Num,
		NodeCount:      cluster.Properties.NodeStatus.Total,
		ReadyNodes:     cluster.Properties.NodeStatus.Ready,
		IdleNodes:      cluster.Properties.NodeStatus.Idle,
		UnhealthyNodes: cluster.Properties.NodeStatus.Unhealthy,
		Subscription:   cluster.Subscription,
		ResourceGroup:  cluster.ResourceGroup,
		Region:         cluster.Region,
		Profile:        cluster.ProfileName,
		CreatedAt:      formatSSPTime(cluster.CreateTime),
		UpdatedAt:      formatSSPTime(cluster.UpdateTime),
	}
}

func matchSSPCluster(identifier string, items []platform.SSPCluster) (platform.SSPCluster, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	exact := make([]platform.SSPCluster, 0, 1)
	fuzzy := make([]platform.SSPCluster, 0, 1)
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		uid := strings.ToLower(strings.TrimSpace(item.UID))
		if identifier == name || identifier == uid {
			exact = append(exact, item)
		} else if strings.Contains(name, identifier) || strings.Contains(uid, identifier) {
			fuzzy = append(fuzzy, item)
		}
	}
	selected := exact
	if len(selected) == 0 {
		selected = fuzzy
	}
	if len(selected) == 1 {
		return selected[0], nil
	}
	if len(selected) > 1 {
		names := make([]string, 0, len(selected))
		for _, item := range selected {
			names = append(names, item.Name)
		}
		sort.Strings(names)
		return platform.SSPCluster{}, fmt.Errorf("cluster %q matched multiple SSP clusters: %s", identifier, strings.Join(names, ", "))
	}
	return platform.SSPCluster{}, fmt.Errorf("SSP cluster %q not found", identifier)
}

func (s *SSPResourceService) ListWorkspaces(ctx context.Context, region string) (*SSPWorkspaceListResult, error) {
	workspaces, err := s.platform.ListSSPWorkspaces(ctx, s.resolveRegion(region))
	if err != nil {
		return nil, fmt.Errorf("list SSP workspaces: %w", err)
	}
	items := boundedMap(ctx, workspaces, 6, func(ctx context.Context, workspace platform.SSPWorkspace) SSPWorkspaceItem {
		return s.workspaceItem(ctx, workspace)
	})
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	return &SSPWorkspaceListResult{Items: items}, nil
}

func (s *SSPResourceService) GetWorkspace(ctx context.Context, identifier string, region string) (*SSPWorkspaceItem, error) {
	workspaces, err := s.platform.ListSSPWorkspaces(ctx, s.resolveRegion(region))
	if err != nil {
		return nil, fmt.Errorf("list SSP workspaces: %w", err)
	}
	workspace, err := matchSSPWorkspace(identifier, workspaces)
	if err != nil {
		return nil, err
	}
	item := s.workspaceItem(ctx, workspace)
	queues, err := s.platform.ListSSPQueues(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("list queues for workspace %s: %w", workspace.Name, err)
	}
	item.Queues = make([]SSPQueueItem, 0, len(queues))
	for _, queue := range queues {
		item.Queues = append(item.Queues, s.queueItem(ctx, workspace, queue, item.VCluster, false))
	}
	sortSSPQueueItems(item.Queues)
	return &item, nil
}

func (s *SSPResourceService) workspaceItem(ctx context.Context, workspace platform.SSPWorkspace) SSPWorkspaceItem {
	item := SSPWorkspaceItem{
		Name:          workspace.Name,
		UID:           "-",
		State:         workspace.State,
		VCluster:      firstNonEmpty(workspace.ClusterName, workspace.ClusterUID),
		Subscription:  workspace.Subscription,
		ResourceGroup: workspace.ResourceGroup,
		Region:        workspace.Region,
		Profile:       workspace.ProfileName,
		CreatedAt:     formatSSPTime(workspace.CreateTime),
		UpdatedAt:     formatSSPTime(workspace.UpdateTime),
	}
	workspaceQueueUID, resolvedVC := s.resolveWorkspaceRuntime(ctx, workspace)
	item.UID = workspaceQueueUID
	if item.VCluster == "" {
		item.VCluster = resolvedVC
	}
	return item
}

func (s *SSPResourceService) ListQueues(ctx context.Context, region string) (*SSPQueueListResult, error) {
	resolvedRegion := s.resolveRegion(region)
	workspaces, err := s.platform.ListSSPWorkspaces(ctx, resolvedRegion)
	if err != nil {
		return nil, fmt.Errorf("list SSP workspaces: %w", err)
	}

	profileNames := make([]string, 0)
	seenProfiles := make(map[string]struct{})
	for _, workspace := range workspaces {
		name := strings.TrimSpace(workspace.ProfileName)
		if name == "" {
			continue
		}
		if _, exists := seenProfiles[name]; !exists {
			seenProfiles[name] = struct{}{}
			profileNames = append(profileNames, name)
		}
	}
	type resourceLoad struct {
		items []platform.SSPQueueResourceDetails
		err   error
	}
	loads := boundedMap(ctx, profileNames, 4, func(ctx context.Context, profileName string) resourceLoad {
		items, queryErr := s.platform.ListSSPQueueResources(ctx, profileName, resolvedRegion)
		return resourceLoad{items: items, err: queryErr}
	})

	items := make([]SSPQueueItem, 0)
	mappedWorkspaces := make(map[string]struct{})
	for _, load := range loads {
		if load.err != nil {
			continue
		}
		for _, details := range load.items {
			candidates := likelyQueueWorkspaces(details.Name, workspaces)
			matched := candidates[:0]
			for _, workspace := range candidates {
				if strings.EqualFold(workspace.ProfileName, details.ProfileName) {
					matched = append(matched, workspace)
				}
			}
			if len(matched) != 1 {
				continue
			}
			workspace := matched[0]
			items = append(items, queueItemFromResource(workspace, details))
			mappedWorkspaces[sspWorkspaceKey(workspace)] = struct{}{}
		}
	}

	remaining := make([]platform.SSPWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if _, mapped := mappedWorkspaces[sspWorkspaceKey(workspace)]; !mapped {
			remaining = append(remaining, workspace)
		}
	}
	if len(remaining) > 0 {
		fallback, fallbackErr := s.listQueuesForWorkspaces(ctx, remaining)
		if fallbackErr != nil && len(items) == 0 {
			return nil, fallbackErr
		}
		if fallback != nil {
			items = append(items, fallback.Items...)
		}
	}
	items = deduplicateSSPQueueItems(items)
	sortSSPQueueItems(items)
	return &SSPQueueListResult{Items: items}, nil
}

func queueItemFromResource(workspace platform.SSPWorkspace, details platform.SSPQueueResourceDetails) SSPQueueItem {
	return SSPQueueItem{
		Name:          details.Name,
		UID:           details.UID,
		State:         details.State,
		Type:          details.Type,
		Workspace:     workspace.Name,
		WorkspaceUID:  workspace.UID,
		Cluster:       details.ClusterName,
		ClusterUID:    details.ClusterUID,
		VCluster:      firstNonEmpty(details.VClusterName, workspace.ClusterName, workspace.ClusterUID),
		Subscription:  firstNonEmpty(details.Subscription, workspace.Subscription),
		ResourceGroup: firstNonEmpty(details.ResourceGroup, workspace.ResourceGroup),
		Region:        firstNonEmpty(details.Region, workspace.Region),
		Profile:       firstNonEmpty(details.ProfileName, workspace.ProfileName),
		CreatedAt:     formatSSPTime(details.CreateTime),
		UpdatedAt:     formatSSPTime(details.UpdateTime),
		NodeCount:     len(details.NodeNames),
	}
}

func sspWorkspaceKey(workspace platform.SSPWorkspace) string {
	return strings.ToLower(strings.Join([]string{workspace.ProfileName, workspace.Subscription, workspace.Name}, "|"))
}

func deduplicateSSPQueueItems(items []SSPQueueItem) []SSPQueueItem {
	result := make([]SSPQueueItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := strings.ToLower(strings.Join([]string{item.Profile, firstNonEmpty(item.UID, item.Name)}, "|"))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func (s *SSPResourceService) listQueuesForWorkspaces(ctx context.Context, workspaces []platform.SSPWorkspace) (*SSPQueueListResult, error) {
	type workspaceQueues struct {
		workspace platform.SSPWorkspace
		queues    []platform.SSPQueue
		err       error
	}
	loads := boundedMap(ctx, workspaces, 20, func(ctx context.Context, workspace platform.SSPWorkspace) workspaceQueues {
		queues, queryErr := s.platform.ListSSPQueues(ctx, workspace)
		return workspaceQueues{workspace: workspace, queues: queues, err: queryErr}
	})
	validLoads := make([]workspaceQueues, 0, len(loads))
	var firstErr error
	for _, load := range loads {
		if load.err != nil {
			if firstErr == nil {
				firstErr = load.err
			}
			continue
		}
		validLoads = append(validLoads, load)
	}
	if len(validLoads) == 0 && firstErr != nil {
		return nil, fmt.Errorf("list SSP queues: %w", firstErr)
	}
	queueGroups := boundedMap(ctx, validLoads, 20, func(ctx context.Context, load workspaceQueues) []SSPQueueItem {
		vc := s.resolveWorkspaceVCluster(ctx, load.workspace)
		items := make([]SSPQueueItem, 0, len(load.queues))
		for _, queue := range load.queues {
			items = append(items, s.queueItem(ctx, load.workspace, queue, vc, false))
		}
		return items
	})
	items := make([]SSPQueueItem, 0)
	for _, group := range queueGroups {
		items = append(items, group...)
	}
	sortSSPQueueItems(items)
	return &SSPQueueListResult{Items: items}, nil
}

func (s *SSPResourceService) GetQueue(ctx context.Context, identifier string, region string, includeDetails bool) (*SSPQueueItem, error) {
	startedAt := time.Now()
	resolvedRegion := firstNonEmpty(strings.TrimSpace(region), inferSSPRegionFromResourceName(identifier))
	resourceStartedAt := time.Now()
	details, directErr := s.platform.FindSSPQueueResource(ctx, resolvedRegion, identifier)
	resourceElapsed := time.Since(resourceStartedAt)
	if directErr == nil {
		item := queueItemFromResourceDetails(*details)
		item.Timings.ResourceLookup = resourceElapsed
		// Keep the fast RMH path when possible. Older queue resources omit the
		// lending policy, so only those queues need the slower detail request.
		if reasons := queueDetailReasons(*details, includeDetails); len(reasons) > 0 {
			item.Timings.DetailReason = strings.Join(reasons, ",")
			detailStartedAt := time.Now()
			if err := s.enrichQueueSchedulingSettings(ctx, &item); err != nil {
				return nil, err
			}
			item.Timings.DetailLookup = time.Since(detailStartedAt)
		}
		item.Timings.Total = time.Since(startedAt)
		return &item, nil
	}

	fallbackStartedAt := time.Now()
	item, err := s.getQueue(ctx, identifier, resolvedRegion, includeDetails)
	if err != nil {
		return nil, errors.Join(directErr, err)
	}
	item.Timings.ResourceLookup = resourceElapsed
	item.Timings.FallbackLookup = time.Since(fallbackStartedAt)
	missingSpotLending := strings.TrimSpace(item.SpotLending) == "" || item.SpotLending == "-"
	if includeDetails || missingSpotLending {
		if missingSpotLending {
			item.Timings.DetailReason = "spot-lending"
		} else {
			item.Timings.DetailReason = "full-details"
		}
		detailStartedAt := time.Now()
		if err := s.enrichQueueSchedulingSettings(ctx, item); err != nil {
			return nil, err
		}
		item.Timings.DetailLookup = time.Since(detailStartedAt)
	}
	item.Timings.Total = time.Since(startedAt)
	return item, nil
}

func queueDetailReasons(details platform.SSPQueueResourceDetails, includeDetails bool) []string {
	reasons := make([]string, 0, 2)
	if details.SpotLending == nil {
		reasons = append(reasons, "spot-lending")
	}
	if includeDetails && !details.NodeCountKnown {
		reasons = append(reasons, "node-count")
	}
	return reasons
}

func queueItemFromResourceDetails(details platform.SSPQueueResourceDetails) SSPQueueItem {
	return SSPQueueItem{
		Name:          details.Name,
		UID:           details.UID,
		State:         details.State,
		Type:          details.Type,
		Workspace:     firstNonEmpty(details.WorkspaceName, inferWorkspaceNameFromQueue(details.Name)),
		WorkspaceUID:  details.WorkspaceUID,
		Cluster:       details.ClusterName,
		ClusterUID:    details.ClusterUID,
		VCluster:      details.VClusterName,
		Subscription:  details.Subscription,
		ResourceGroup: details.ResourceGroup,
		Region:        details.Region,
		Profile:       details.ProfileName,
		CreatedAt:     formatSSPTime(details.CreateTime),
		UpdatedAt:     formatSSPTime(details.UpdateTime),
		NodeCount:     details.NodeCount,
		SpotLending:   formatSSPSpotLending(details.SpotLending),
		DequeuePolicy: formatSSPDequeuePolicy(details.DequeuePolicy),
	}
}

func (s *SSPResourceService) enrichQueueSchedulingSettings(ctx context.Context, item *SSPQueueItem) error {
	if err := s.ensureQueuePlatformLocation(ctx, item); err != nil {
		return err
	}
	detail, err := s.platform.GetSSPQueue(ctx, item.Profile, item.Subscription, item.ResourceGroup, item.Region, item.Cluster, item.Name)
	if err != nil {
		return fmt.Errorf("get queue %s detail: %w", item.Name, err)
	}
	item.Workspace = firstNonEmpty(item.Workspace, detail.WorkspaceName, detail.Properties.Workspace.Name)
	item.WorkspaceUID = firstNonEmpty(item.WorkspaceUID, detail.Properties.Workspace.UID)
	if detail.Properties.NodeStatus.Total > 0 {
		item.NodeCount = detail.Properties.NodeStatus.Total
	}
	item.SpotLending = formatSSPSpotLending(detail.Properties.AdvancedSettings.ProvideSpotResourceEnabled)
	item.DequeuePolicy = formatSSPDequeuePolicy(detail.Properties.AdvancedSettings.DequeueStrategy)
	return nil
}

func (s *SSPResourceService) ListQueueWorkloads(ctx context.Context, identifier string, region string, query SSPQueueWorkloadQuery) (*SSPQueueWorkloadResult, error) {
	queue, err := s.resolveQueueForNodes(ctx, identifier, region)
	if err != nil {
		return nil, err
	}
	if err := s.ensureQueuePlatformLocation(ctx, queue); err != nil {
		return nil, err
	}
	workloads, err := s.platform.ListSSPQueueWorkloads(ctx, queue.Profile, queue.Subscription, queue.ResourceGroup, queue.Region, queue.Cluster, queue.Name, platform.SSPQueueWorkloadQuery{
		Type: query.Type, State: query.State, Priority: query.Priority,
	})
	if err != nil {
		return nil, fmt.Errorf("list workloads for queue %s: %w", queue.Name, err)
	}
	items := make([]SSPQueueWorkloadItem, 0, len(workloads))
	for _, workload := range workloads {
		items = append(items, SSPQueueWorkloadItem{
			Queue:     queue.Name,
			Type:      workload.Type,
			Name:      firstNonEmpty(workload.Name, workload.DisplayName),
			UID:       workload.UID,
			State:     workload.State,
			Workspace: workload.Workspace.Name,
			Priority:  workload.Priority,
			Resources: formatSSPWorkloadResources(workload),
			Creator:   workload.Ownership.CreatorName,
			CreatorID: workload.Ownership.CreatorID,
			CreatedAt: formatSSPTime(workload.CreateTime),
		})
	}
	return &SSPQueueWorkloadResult{Queue: *queue, Items: items}, nil
}

func (s *SSPResourceService) getQueue(ctx context.Context, identifier string, region string, includeNodeCount bool) (*SSPQueueItem, error) {
	workspaces, err := s.platform.ListSSPWorkspaces(ctx, s.resolveRegion(region))
	if err != nil {
		return nil, fmt.Errorf("list SSP workspaces: %w", err)
	}
	if candidates := likelyQueueWorkspaces(identifier, workspaces); len(candidates) > 0 {
		result, queryErr := s.listQueuesForWorkspaces(ctx, candidates)
		if queryErr == nil {
			if item, matchErr := matchSSPQueue(identifier, result.Items); matchErr == nil {
				if includeNodeCount && item.NodeCount == 0 {
					nodes, _ := s.listQueueNodes(ctx, item.UID)
					item.NodeCount = len(nodes)
				}
				return &item, nil
			}
		}
	}
	result, err := s.listQueuesForWorkspaces(ctx, workspaces)
	if err != nil {
		return nil, err
	}
	item, err := matchSSPQueue(identifier, result.Items)
	if err != nil {
		return nil, err
	}
	if includeNodeCount && item.NodeCount == 0 {
		nodes, _ := s.listQueueNodes(ctx, item.UID)
		item.NodeCount = len(nodes)
	}
	return &item, nil
}

func likelyQueueWorkspaces(identifier string, workspaces []platform.SSPWorkspace) []platform.SSPWorkspace {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	if identifier == "" || !strings.HasPrefix(identifier, "queue-") {
		return nil
	}
	candidateNames := map[string]struct{}{
		"ws-" + strings.TrimPrefix(identifier, "queue-"): {},
	}
	for _, marker := range []string{"-reserved-", "-exclusive-", "-elastic-", "-shared-", "-spot-", "-ondemand-", "-on-demand-"} {
		if strings.Contains(identifier, marker) {
			candidateNames["ws-"+strings.Replace(strings.TrimPrefix(identifier, "queue-"), strings.TrimPrefix(marker, "-"), "", 1)] = struct{}{}
		}
	}
	result := make([]platform.SSPWorkspace, 0, len(candidateNames))
	for _, workspace := range workspaces {
		if _, ok := candidateNames[strings.ToLower(strings.TrimSpace(workspace.Name))]; ok {
			result = append(result, workspace)
		}
	}
	return result
}

func (s *SSPResourceService) ListQueueNodes(ctx context.Context, identifier string, region string) (*SSPQueueNodeListResult, error) {
	queue, err := s.resolveQueueForNodes(ctx, identifier, region)
	if err != nil {
		return nil, err
	}
	if items, ok := s.queueNodeListItemsFromKube(ctx, *queue); ok {
		queue.NodeCount = len(items)
		return &SSPQueueNodeListResult{Queue: *queue, Items: items}, nil
	}
	nodes, err := s.platformQueueNodes(ctx, queue)
	if err != nil {
		return nil, err
	}
	items := s.queueNodeListItems(ctx, nodes, true)
	queue.NodeCount = len(items)
	return &SSPQueueNodeListResult{Queue: *queue, Items: items}, nil
}

func (s *SSPResourceService) queueNodeListItemsFromKube(ctx context.Context, queue SSPQueueItem) ([]VCNodeListItem, bool) {
	if s.queueNodeClientResolver == nil || strings.TrimSpace(queue.UID) == "" {
		return nil, false
	}
	clientset, err := s.queueNodeClientResolver(queue)
	if err != nil || clientset == nil {
		return nil, false
	}
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector:   labels.Set(map[string]string{sspQueueUIDLabel: queue.UID}).AsSelector().String(),
		ResourceVersion: "0",
	})
	if err != nil || len(nodes.Items) == 0 {
		return nil, false
	}
	items := make([]VCNodeListItem, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		items = append(items, queueNodeListItem(node))
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].HostName) < strings.ToLower(items[j].HostName) })
	return items, true
}

func (s *SSPResourceService) GetQueueNodeUsage(ctx context.Context, identifier string, region string) (*SSPQueueNodeUsageResult, error) {
	queue, err := s.resolveQueueForNodes(ctx, identifier, region)
	if err != nil {
		return nil, err
	}
	nodes, err := s.platformQueueNodes(ctx, queue)
	if err != nil {
		return nil, err
	}
	nodeItems := s.queueNodeListItems(ctx, nodes, false)
	items := make([]SSPQueueNodeUsageItem, 0, len(nodes))
	for index, node := range nodes {
		cpuAllocated, cpuTotal, _ := sspQueueNodeResource(node, "CPU")
		memoryAllocated, memoryTotal, _ := sspQueueNodeResource(node, "MEMORY")
		acceleratorAllocated, acceleratorTotal, acceleratorFree := sspQueueNodeResource(node, "DEVICE")
		items = append(items, SSPQueueNodeUsageItem{
			HostName:             nodeItems[index].HostName,
			HostIP:               strings.TrimSpace(node.HostIP),
			State:                strings.TrimSpace(node.State),
			CPUAllocated:         cpuAllocated,
			CPUTotal:             cpuTotal,
			MemoryAllocated:      memoryAllocated,
			MemoryTotal:          memoryTotal,
			AcceleratorAllocated: acceleratorAllocated,
			AcceleratorTotalText: acceleratorTotal,
			AcceleratorFree:      int64(firstSSPResourceNumber(acceleratorFree)),
			AcceleratorTotal:     int64(firstSSPResourceNumber(acceleratorTotal)),
		})
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].HostIP) < strings.ToLower(items[j].HostIP) })
	queue.NodeCount = len(nodes)
	return &SSPQueueNodeUsageResult{Queue: *queue, Items: items}, nil
}

func (s *SSPResourceService) resolveQueueForNodes(ctx context.Context, identifier string, region string) (*SSPQueueItem, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		queue *SSPQueueItem
		err   error
	}
	results := make(chan result, 2)
	go func() {
		queue, err := s.getQueue(ctx, identifier, region, false)
		results <- result{queue: queue, err: err}
	}()
	go func() {
		details, err := s.platform.FindSSPQueueResource(ctx, s.resolveRegion(region), identifier)
		if err != nil {
			results <- result{err: err}
			return
		}
		results <- result{queue: &SSPQueueItem{
			Name:          details.Name,
			UID:           details.UID,
			State:         details.State,
			Type:          details.Type,
			Workspace:     inferWorkspaceNameFromQueue(details.Name),
			Cluster:       details.ClusterName,
			ClusterUID:    details.ClusterUID,
			VCluster:      details.VClusterName,
			Subscription:  details.Subscription,
			ResourceGroup: details.ResourceGroup,
			Region:        details.Region,
			Profile:       details.ProfileName,
			CreatedAt:     formatSSPTime(details.CreateTime),
			UpdatedAt:     formatSSPTime(details.UpdateTime),
			NodeCount:     len(details.NodeNames),
		}}
	}()

	var firstErr error
	for range 2 {
		resolved := <-results
		if resolved.err == nil && resolved.queue != nil {
			cancel()
			return resolved.queue, nil
		}
		if firstErr == nil {
			firstErr = resolved.err
		}
	}
	return nil, firstErr
}

func inferWorkspaceNameFromQueue(queueName string) string {
	base := strings.TrimPrefix(strings.TrimSpace(queueName), "queue-")
	for _, marker := range []string{"-reserved-", "-exclusive-", "-elastic-", "-shared-", "-spot-", "-ondemand-", "-on-demand-"} {
		if strings.Contains(base, marker) {
			return "ws-" + strings.Replace(base, marker, "-", 1)
		}
	}
	return "-"
}

func inferSSPRegionFromResourceName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(name, "queue-d-"), strings.HasPrefix(name, "ws-d-"):
		return "cn-pj-01"
	case strings.HasPrefix(name, "queue-t-"), strings.HasPrefix(name, "ws-t-"):
		return "cn-pj-03"
	default:
		return ""
	}
}

func firstSSPResourceNumber(value string) float64 {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return 0
	}
	result, _ := strconv.ParseFloat(fields[0], 64)
	return result
}

func formatSSPSpotLending(enabled *bool) string {
	if enabled == nil {
		return "-"
	}
	if *enabled {
		return "开启"
	}
	return "关闭"
}

func formatSSPDequeuePolicy(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "HIGH_UTILIZATION", "":
		if strings.TrimSpace(value) == "" {
			return "高利用率（默认）"
		}
		return "高利用率"
	case "STRONG_PRIORITY":
		return "强优先级"
	case "BALANCED":
		return "均衡"
	default:
		return strings.TrimSpace(value)
	}
}

func formatSSPWorkloadResources(workload platform.SSPQueueWorkload) string {
	parts := make([]string, 0, len(workload.Tasks))
	for _, task := range workload.Tasks {
		resourceParts := make([]string, 0, 4)
		if cpu := compactSSPValue(task.Resource.CPU); cpu != "" {
			resourceParts = append(resourceParts, cpu+"C")
		}
		if memory := compactSSPValue(task.Resource.Memory); memory != "" {
			resourceParts = append(resourceParts, memory)
		}
		if accelerator := compactSSPValue(task.Resource.AccelerateDeviceCount); accelerator != "" && accelerator != "0" {
			resourceParts = append(resourceParts, accelerator+"ACC")
		}
		if len(task.Resource.MachineTypes) > 0 {
			resourceParts = append(resourceParts, strings.Join(task.Resource.MachineTypes, ","))
		}
		name := firstNonEmpty(strings.TrimSpace(task.Name), "task")
		replicas := task.Replicas
		if replicas <= 0 {
			replicas = 1
		}
		parts = append(parts, fmt.Sprintf("%s×%d %s", name, replicas, strings.Join(resourceParts, "/")))
	}
	return strings.TrimSpace(strings.Join(parts, "; "))
}

func compactSSPValue(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	text = strings.TrimSuffix(text, ".0")
	for _, unit := range []string{"GiB", "MiB", "KiB"} {
		text = strings.Replace(text, ".0"+unit, unit, 1)
	}
	return text
}

func (s *SSPResourceService) ensureQueuePlatformLocation(ctx context.Context, queue *SSPQueueItem) error {
	if queue.Cluster != "" && queue.Cluster != "-" && queue.Subscription != "" && queue.Region != "" && queue.Profile != "" {
		return nil
	}
	details, err := s.platform.GetSSPQueueResource(ctx, queue.Profile, queue.Name)
	if err != nil {
		return fmt.Errorf("resolve queue resource %s: %w", queue.Name, err)
	}
	queue.Cluster = firstNonEmpty(queue.Cluster, details.ClusterName)
	queue.ClusterUID = firstNonEmpty(queue.ClusterUID, details.ClusterUID)
	queue.VCluster = firstNonEmpty(queue.VCluster, details.VClusterName)
	queue.Subscription = firstNonEmpty(queue.Subscription, details.Subscription)
	queue.ResourceGroup = firstNonEmpty(queue.ResourceGroup, details.ResourceGroup)
	queue.Region = firstNonEmpty(queue.Region, details.Region)
	queue.Profile = firstNonEmpty(queue.Profile, details.ProfileName)
	if queue.Cluster == "" || queue.Cluster == "-" {
		return fmt.Errorf("queue %s has no SSP cluster", queue.Name)
	}
	return nil
}

func (s *SSPResourceService) platformQueueNodes(ctx context.Context, queue *SSPQueueItem) ([]platform.SSPQueueNode, error) {
	if err := s.ensureQueuePlatformLocation(ctx, queue); err != nil {
		return nil, err
	}
	items, err := s.platform.ListSSPQueueNodes(ctx, queue.Profile, queue.Subscription, queue.ResourceGroup, queue.Region, queue.Cluster, queue.Name)
	if err != nil {
		return nil, fmt.Errorf("list nodes for queue %s cluster %s: %w", queue.Name, queue.Cluster, err)
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].HostIP) < strings.ToLower(items[j].HostIP) })
	return items, nil
}

func (s *SSPResourceService) queueNodeListItems(ctx context.Context, nodes []platform.SSPQueueNode, enrichModel bool) []VCNodeListItem {
	items := make([]VCNodeListItem, 0, len(nodes))
	for _, node := range nodes {
		hostName := ""
		if strings.Count(node.HostIP, ".") == 3 {
			hostName = "host-" + strings.ReplaceAll(strings.TrimSpace(node.HostIP), ".", "-")
		}
		items = append(items, VCNodeListItem{
			Kind:        "ACN",
			UID:         strings.TrimSpace(node.UID),
			Name:        firstNonEmpty(strings.TrimSpace(node.Name), strings.TrimSpace(node.DisplayName), strings.TrimSpace(node.UID)),
			HostName:    hostName,
			HostIP:      strings.TrimSpace(node.HostIP),
			State:       strings.TrimSpace(node.State),
			Zone:        strings.TrimSpace(node.Zone),
			MachineType: strings.TrimSpace(node.MachineType),
		})
	}
	if !enrichModel || s.clientset == nil || len(items) == 0 {
		return items
	}

	type modelLookup struct {
		key      string
		hostName string
	}
	representatives := make([]modelLookup, 0)
	seenMachineTypes := make(map[string]struct{})
	for _, item := range items {
		if item.HostName == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(item.MachineType))
		if key == "" {
			key = "__unknown__"
		}
		if _, exists := seenMachineTypes[key]; exists {
			continue
		}
		seenMachineTypes[key] = struct{}{}
		representatives = append(representatives, modelLookup{key: key, hostName: item.HostName})
	}
	type modelResult struct {
		key   string
		model string
	}
	models := boundedMap(ctx, representatives, 8, func(queryCtx context.Context, lookup modelLookup) modelResult {
		node, err := s.clientset.CoreV1().Nodes().Get(queryCtx, lookup.hostName, metav1.GetOptions{})
		if err != nil {
			return modelResult{key: lookup.key}
		}
		return modelResult{key: lookup.key, model: queueNodeListItem(*node).Model}
	})
	modelByMachineType := make(map[string]string, len(models))
	for _, result := range models {
		if result.model != "" {
			modelByMachineType[result.key] = result.model
		}
	}
	for index := range items {
		key := strings.ToLower(strings.TrimSpace(items[index].MachineType))
		if key == "" {
			key = "__unknown__"
		}
		items[index].Model = modelByMachineType[key]
	}
	return items
}

func sspQueueNodeResource(node platform.SSPQueueNode, resourceType string) (allocated string, total string, unallocated string) {
	for _, summary := range node.SummaryData {
		if strings.EqualFold(strings.TrimSpace(summary.ResourceType), strings.TrimSpace(resourceType)) {
			return strings.TrimSpace(summary.Allocated), strings.TrimSpace(summary.Total), strings.TrimSpace(summary.Unallocated)
		}
	}
	return "", "", ""
}

func (s *SSPResourceService) listQueueNodes(ctx context.Context, queueUID string) ([]corev1.Node, error) {
	queueUID = strings.TrimSpace(queueUID)
	if queueUID == "" {
		return nil, fmt.Errorf("queue uid is empty; cannot resolve queue nodes")
	}
	list, err := s.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector:   labels.Set(map[string]string{sspQueueUIDLabel: queueUID}).AsSelector().String(),
		ResourceVersion: "0",
	})
	if err != nil {
		return nil, fmt.Errorf("list nodes for queue %s: %w", queueUID, err)
	}
	sort.Slice(list.Items, func(i, j int) bool { return strings.ToLower(list.Items[i].Name) < strings.ToLower(list.Items[j].Name) })
	return list.Items, nil
}

func (s *SSPResourceService) resolveWorkspaceVCluster(ctx context.Context, workspace platform.SSPWorkspace) string {
	_, vc := s.resolveWorkspaceRuntime(ctx, workspace)
	return vc
}

func (s *SSPResourceService) resolveWorkspaceRuntime(ctx context.Context, workspace platform.SSPWorkspace) (string, string) {
	if s.clientset == nil {
		return "-", "-"
	}
	namespaces, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector:   labels.Set(map[string]string{sspWorkspaceNameLabel: workspace.Name}).AsSelector().String(),
		ResourceVersion: "0",
		Limit:           20,
	})
	if err != nil || len(namespaces.Items) == 0 {
		return "-", "-"
	}
	namespace := namespaces.Items[0]
	workspaceQueueUID := strings.TrimSpace(namespace.Labels[sspWorkspaceQueueUIDLabel])
	if workspaceQueueUID == "" {
		workspaceQueueUID = namespace.Name
	}
	vc := s.sspBase.resolveSSPVClusterName(ctx, nil, namespace.Name, workspace.ProfileName)
	return workspaceQueueUID, vc
}

func (s *SSPResourceService) queueItem(ctx context.Context, workspace platform.SSPWorkspace, queue platform.SSPQueue, vc string, includeNodeCount bool) SSPQueueItem {
	item := SSPQueueItem{
		Name:          firstNonEmpty(queue.Name, queue.DisplayName),
		UID:           queue.UID,
		State:         queue.State,
		Type:          queue.Type,
		Workspace:     firstNonEmpty(queue.WorkspaceName, workspace.Name),
		WorkspaceUID:  workspace.UID,
		Cluster:       firstNonEmpty(queue.Properties.ClusterName, queue.Properties.Cluster.Name, queue.Spec.ClusterName),
		ClusterUID:    firstNonEmpty(queue.Properties.ClusterUID, queue.Properties.Cluster.UID, queue.Spec.ClusterUID),
		VCluster:      vc,
		Subscription:  firstNonEmpty(queue.SubscriptionName, workspace.Subscription),
		ResourceGroup: firstNonEmpty(queue.ResourceGroup, workspace.ResourceGroup),
		Region:        firstNonEmpty(queue.Region, workspace.Region),
		Profile:       firstNonEmpty(queue.ProfileName, workspace.ProfileName),
		CreatedAt:     formatSSPTime(queue.CreateTime),
		UpdatedAt:     formatSSPTime(queue.UpdateTime),
		NodeCount:     queue.Properties.NodeStatus.Total,
	}
	if includeNodeCount && item.NodeCount == 0 {
		if nodes, err := s.listQueueNodes(ctx, item.UID); err == nil {
			item.NodeCount = len(nodes)
		}
	}
	return item
}

func queueNodeListItem(node corev1.Node) VCNodeListItem {
	return VCNodeListItem{
		Kind:        "ACN",
		UID:         strings.TrimSpace(node.Labels["resource.compute.sensecore.cn/acn-uid"]),
		Name:        node.Name,
		HostName:    node.Name,
		HostIP:      firstNonEmpty(hostIPFromNodeName(node.Name), nodeInternalIP(node.Status.Addresses)),
		State:       nodeReadyStatus(node.Status.Conditions),
		Zone:        strings.TrimSpace(node.Labels[sspNodeZoneLabel]),
		MachineType: strings.TrimSpace(node.Labels[sspMachineTypeLabel]),
		Model: firstNonEmpty(
			strings.TrimSpace(node.Labels["accelerator-type"]),
			strings.TrimSpace(node.Labels["node.kubernetes.io/npu.chip.name"]),
			strings.TrimSpace(node.Labels["accelerator"]),
		),
	}
}

func hostIPFromNodeName(nodeName string) string {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(nodeName), "host-"), "-")
	if len(parts) != 4 {
		return ""
	}
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 255 {
			return ""
		}
	}
	return strings.Join(parts, ".")
}

func (s *SSPResourceService) resolveRegion(requested string) string {
	return strings.TrimSpace(requested)
}

func matchSSPWorkspace(identifier string, items []platform.SSPWorkspace) (platform.SSPWorkspace, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	exact := make([]platform.SSPWorkspace, 0, 1)
	fuzzy := make([]platform.SSPWorkspace, 0, 1)
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		uid := strings.ToLower(strings.TrimSpace(item.UID))
		if identifier == name || identifier == uid {
			exact = append(exact, item)
		} else if strings.Contains(name, identifier) || strings.Contains(uid, identifier) {
			fuzzy = append(fuzzy, item)
		}
	}
	return chooseSSPWorkspace(identifier, exact, fuzzy)
}

func chooseSSPWorkspace(identifier string, exact []platform.SSPWorkspace, fuzzy []platform.SSPWorkspace) (platform.SSPWorkspace, error) {
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return platform.SSPWorkspace{}, fmt.Errorf("workspace %q matched multiple profiles", identifier)
	}
	if len(fuzzy) == 1 {
		return fuzzy[0], nil
	}
	if len(fuzzy) > 1 {
		return platform.SSPWorkspace{}, fmt.Errorf("workspace %q matched multiple workspaces", identifier)
	}
	return platform.SSPWorkspace{}, fmt.Errorf("workspace %q not found", identifier)
}

func matchSSPQueue(identifier string, items []SSPQueueItem) (SSPQueueItem, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	exact := make([]SSPQueueItem, 0, 1)
	fuzzy := make([]SSPQueueItem, 0, 1)
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		uid := strings.ToLower(strings.TrimSpace(item.UID))
		if identifier == name || identifier == uid || identifier == "ssp-"+uid {
			exact = append(exact, item)
		} else if strings.Contains(name, identifier) || strings.Contains(uid, strings.TrimPrefix(identifier, "ssp-")) {
			fuzzy = append(fuzzy, item)
		}
	}
	selected := exact
	if len(selected) == 0 {
		selected = fuzzy
	}
	if len(selected) == 1 {
		return selected[0], nil
	}
	if len(selected) > 1 {
		values := make([]string, 0, len(selected))
		for _, item := range selected {
			values = append(values, item.Workspace+"/"+item.Name)
		}
		sort.Strings(values)
		return SSPQueueItem{}, fmt.Errorf("queue %q matched multiple queues: %s", identifier, strings.Join(values, ", "))
	}
	return SSPQueueItem{}, fmt.Errorf("queue %q not found", identifier)
}

func sortSSPQueueItems(items []SSPQueueItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Workspace != items[j].Workspace {
			return strings.ToLower(items[i].Workspace) < strings.ToLower(items[j].Workspace)
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
}
