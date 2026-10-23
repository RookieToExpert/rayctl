package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"rayctl/internal/platform"
)

type SSPWorkspaceWorkloadResult struct {
	Workspace SSPWorkspaceItem
	Items     []SSPQueueWorkloadItem
	Warnings  []string
}

// ListWorkspaceWorkloads aggregates queue workload APIs and retains the
// workspace boundary even when a queue returns workloads from other workspaces.
func (s *SSPResourceService) ListWorkspaceWorkloads(ctx context.Context, identifier, region string, query SSPQueueWorkloadQuery) (*SSPWorkspaceWorkloadResult, error) {
	if s == nil || s.platform == nil {
		return nil, fmt.Errorf("platform client is required")
	}
	if strings.TrimSpace(identifier) == "" {
		return nil, fmt.Errorf("workspace identifier is required")
	}
	if query.Limit < 0 {
		return nil, fmt.Errorf("workload limit must not be negative")
	}
	workspaces, err := s.platform.ListSSPWorkspaces(ctx, s.resolveRegion(region))
	if err != nil {
		return nil, fmt.Errorf("list SSP workspaces: %w", err)
	}
	workspace, err := matchSSPWorkspace(identifier, workspaces)
	if err != nil {
		return nil, err
	}
	result := &SSPWorkspaceWorkloadResult{Workspace: workspacePlatformItem(workspace), Items: make([]SSPQueueWorkloadItem, 0)}
	queues, err := s.platform.ListSSPQueues(ctx, workspace)
	if err != nil {
		err = fmt.Errorf("list queues for workspace %s: %w", workspace.Name, err)
		result.Warnings = []string{err.Error()}
		return result, err
	}
	queueItems := make([]SSPQueueItem, 0, len(queues))
	for _, queue := range queues {
		queueItems = append(queueItems, s.queueItem(ctx, workspace, queue, "", false))
	}
	result.Workspace.Queues = deduplicateWorkloadQueues(queueItems)
	result.Items, result.Warnings, err = s.collectScopedQueueWorkloads(ctx, result.Workspace.Queues, &workspace, query)
	return result, err
}

func (s *SSPResourceService) collectScopedQueueWorkloads(ctx context.Context, queues []SSPQueueItem, workspace *platform.SSPWorkspace, query SSPQueueWorkloadQuery) ([]SSPQueueWorkloadItem, []string, error) {
	type queueResult struct {
		items []SSPQueueWorkloadItem
		err   error
	}
	loads := boundedMap(ctx, deduplicateWorkloadQueues(queues), 6, func(ctx context.Context, queue SSPQueueItem) queueResult {
		if err := s.ensureQueuePlatformLocation(ctx, &queue); err != nil {
			return queueResult{err: fmt.Errorf("queue %s: %w", queue.Name, err)}
		}
		// Apply the limit after workspace filtering and cross-queue sorting.
		workloads, err := s.platform.ListSSPQueueWorkloads(ctx, queue.Profile, queue.Subscription, queue.ResourceGroup, queue.Region, queue.Cluster, queue.Name, platform.SSPQueueWorkloadQuery{
			Type: query.Type, State: query.State, Priority: query.Priority,
		})
		if err != nil {
			return queueResult{err: fmt.Errorf("list workloads for queue %s: %w", queue.Name, err)}
		}
		items := make([]SSPQueueWorkloadItem, 0, len(workloads))
		for _, workload := range workloads {
			if workspace != nil && !workloadBelongsToWorkspace(workload, queue, *workspace) {
				continue
			}
			items = append(items, scopedQueueWorkloadItem(queue, workload))
		}
		return queueResult{items: items}
	})
	items := make([]SSPQueueWorkloadItem, 0)
	warnings := make([]string, 0)
	errs := make([]error, 0)
	for _, load := range loads {
		if load.err != nil {
			warnings = append(warnings, load.err.Error())
			errs = append(errs, load.err)
		}
		items = append(items, load.items...)
	}
	if err := ctx.Err(); err != nil && len(errs) == 0 {
		warnings = append(warnings, err.Error())
		errs = append(errs, err)
	}
	return sortScopedWorkloads(items, query.Limit), warnings, errors.Join(errs...)
}

func scopedQueueWorkloadItem(queue SSPQueueItem, workload platform.SSPQueueWorkload) SSPQueueWorkloadItem {
	return SSPQueueWorkloadItem{
		Queue: queue.Name, Type: workload.Type, Name: firstNonEmpty(workload.Name, workload.DisplayName), UID: firstNonEmpty(workload.UID, workload.ID),
		State: workload.State, Workspace: firstNonEmpty(workload.Workspace.Name, queue.Workspace), Priority: workload.Priority,
		Resources: formatSSPWorkloadResources(workload), Creator: workload.Ownership.CreatorName, CreatorID: workload.Ownership.CreatorID,
		CreatedAt: formatSSPTime(workload.CreateTime),
	}
}

func workloadBelongsToWorkspace(workload platform.SSPQueueWorkload, queue SSPQueueItem, workspace platform.SSPWorkspace) bool {
	if strings.TrimSpace(workload.Workspace.UID) != "" && strings.TrimSpace(workspace.UID) != "" {
		return strings.EqualFold(strings.TrimSpace(workload.Workspace.UID), strings.TrimSpace(workspace.UID))
	}
	return strings.EqualFold(strings.TrimSpace(firstNonEmpty(workload.Workspace.Name, queue.Workspace)), strings.TrimSpace(workspace.Name))
}

func deduplicateWorkloadQueues(queues []SSPQueueItem) []SSPQueueItem {
	items := make([]SSPQueueItem, 0, len(queues))
	seen := make(map[string]struct{}, len(queues))
	for _, queue := range queues {
		key := strings.ToLower(strings.Join([]string{queue.Profile, queue.Subscription, queue.Region, queue.Cluster, firstNonEmpty(queue.UID, queue.Name)}, "|"))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, queue)
	}
	sortSSPQueueItems(items)
	return items
}

// All items in a scope use one resolved profile. Include workspace in the
// fallback key since different workspaces may reuse a workload name.
func sortScopedWorkloads(items []SSPQueueWorkloadItem, limit int) []SSPQueueWorkloadItem {
	result := make([]SSPQueueWorkloadItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		identity := item.UID
		if strings.TrimSpace(identity) == "" {
			identity = item.Workspace + "|" + item.Namespace + "|" + item.Name
		}
		key := strings.ToLower(item.Type + "|" + identity)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt != result[j].CreatedAt {
			return result[i].CreatedAt > result[j].CreatedAt
		}
		left := strings.ToLower(strings.Join([]string{result[i].Workspace, result[i].Namespace, result[i].Type, result[i].Name, result[i].UID, result[i].Queue}, "|"))
		right := strings.ToLower(strings.Join([]string{result[j].Workspace, result[j].Namespace, result[j].Type, result[j].Name, result[j].UID, result[j].Queue}, "|"))
		return left < right
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}
