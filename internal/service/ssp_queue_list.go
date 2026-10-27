package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"rayctl/internal/platform"
)

func missingQueueWorkspace(name, uid string) bool {
	missing := func(value string) bool {
		value = strings.TrimSpace(value)
		return value == "" || value == "-"
	}
	return missing(name) || missing(uid)
}

type queueListDetailRequest struct {
	index  int
	detail platform.SSPQueueResourceDetails
}

type queueListDetailResult struct {
	index int
	queue *platform.SSPQueue
	err   error
}

// RMH may omit workspace even for a RUNNING queue. Resolve missing names/UIDs
// through the owning queue endpoint, just as queue get resolves its details.
func (s *SSPResourceService) refreshQueueListDetails(ctx context.Context, items []SSPQueueItem, details []platform.SSPQueueResourceDetails, region string) error {
	type scope struct {
		profile, subscription, resourceGroup, region, cluster string
	}
	groups := make([][]queueListDetailRequest, 0)
	groupByScope := make(map[scope]int)
	for index, item := range items {
		if !strings.EqualFold(strings.TrimSpace(item.State), "RUNNING") || missingQueueWorkspace(item.Workspace, item.WorkspaceUID) {
			detail := details[index]
			detail.Region = firstNonEmpty(detail.Region, region)
			key := scope{detail.ProfileName, detail.Subscription, detail.ResourceGroup, detail.Region, detail.ClusterName}
			group, ok := groupByScope[key]
			if !ok {
				group = len(groups)
				groupByScope[key] = group
				groups = append(groups, nil)
			}
			groups[group] = append(groups[group], queueListDetailRequest{index: index, detail: detail})
		}
	}
	groupsLoaded := boundedMap(ctx, groups, 6, func(ctx context.Context, queries []queueListDetailRequest) []queueListDetailResult {
		return s.loadQueueListDetailGroup(ctx, queries)
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	var queryErrors []error
	for _, loads := range groupsLoaded {
		for _, load := range loads {
			item := &items[load.index]
			if load.err != nil {
				queryErrors = append(queryErrors, fmt.Errorf("queue %s detail: %w", item.Name, load.err))
				continue
			}
			if load.queue == nil || !strings.EqualFold(strings.TrimSpace(load.queue.Name), strings.TrimSpace(item.Name)) ||
				(load.queue.UID != "" && item.UID != "" && !strings.EqualFold(load.queue.UID, item.UID)) {
				queryErrors = append(queryErrors, fmt.Errorf("queue %s detail identity does not match its platform resource", item.Name))
				continue
			}
			// Keep the request's exact profile/subscription/cluster identity. Same
			// names in different scopes must never inherit each other's workspace.
			item.State = firstNonEmpty(load.queue.State, item.State)
			mergeQueueWorkspace(item, *load.queue)
		}
	}
	return errors.Join(queryErrors...)
}

func (s *SSPResourceService) loadQueueListDetailGroup(ctx context.Context, queries []queueListDetailRequest) []queueListDetailResult {
	byUID, byName := make(map[string]*platform.SSPQueue), make(map[string]*platform.SSPQueue)
	if len(queries) > 1 {
		detail := queries[0].detail
		queues, err := s.platform.ListSSPClusterQueues(ctx, platform.SSPCluster{
			Name: detail.ClusterName, ProfileName: detail.ProfileName, Subscription: detail.Subscription,
			ResourceGroup: detail.ResourceGroup, Region: detail.Region,
		})
		if err != nil {
			return []queueListDetailResult{{index: queries[0].index, err: err}}
		}
		for index := range queues {
			queue := &queues[index]
			if uid := strings.ToLower(strings.TrimSpace(queue.UID)); uid != "" {
				byUID[uid] = queue
			}
			name := strings.ToLower(strings.TrimSpace(queue.Name))
			if _, exists := byName[name]; exists {
				byName[name] = nil
			} else {
				byName[name] = queue
			}
		}
	}
	results := make([]queueListDetailResult, 0, len(queries))
	for _, query := range queries {
		detail := query.detail
		queue := byUID[strings.ToLower(strings.TrimSpace(detail.UID))]
		if queue == nil {
			queue = byName[strings.ToLower(strings.TrimSpace(detail.Name))]
		}
		var err error
		// Cluster list returns full queue resources on current platforms. Only
		// absent/incomplete entries need an individual detail request.
		if queue == nil || missingQueueWorkspace(queue.WorkspaceName, queue.Properties.Workspace.UID) {
			queue, err = s.platform.GetSSPQueue(ctx, detail.ProfileName, detail.Subscription, detail.ResourceGroup,
				detail.Region, detail.ClusterName, detail.Name)
		}
		results = append(results, queueListDetailResult{index: query.index, queue: queue, err: err})
	}
	return results
}

func mergeQueueWorkspace(item *SSPQueueItem, detail platform.SSPQueue) {
	name := strings.TrimSpace(firstNonEmpty(detail.WorkspaceName, detail.Properties.Workspace.Name))
	uid := strings.TrimSpace(detail.Properties.Workspace.UID)
	if name != "" && name != "-" {
		if !strings.EqualFold(strings.TrimSpace(item.Workspace), name) {
			item.WorkspaceUID = ""
		}
		item.Workspace = name
	}
	if uid != "" && uid != "-" {
		item.WorkspaceUID = uid
	}
}
