package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"rayctl/internal/platform"
)

type UserGetResult struct {
	ID               string
	Username         string
	Name             string
	TenantCode       string
	Status           string
	Source           string
	Groups           []AuthGroupItem
	Jobs             []JobClusterItem
	Workloads        []UserWorkloadItem
	WorkloadWarnings []string
}

type UserWorkloadOptions struct {
	AID                bool
	AIT                bool
	ATE                bool
	AIR                bool
	IncludeInactiveATE bool
}

func (o UserWorkloadOptions) Any() bool {
	return o.AID || o.AIT || o.ATE || o.AIR
}

type UserWorkloadItem struct {
	Type        string
	Name        string
	State       string
	Workspace   string
	Queue       string
	VCluster    string
	Creator     string
	CreatorID   string
	CreatedAt   string
	createdTime time.Time
}

type UserService struct {
	vcClient *platform.VirtualClusterClient
}

func NewUserService(vcClient *platform.VirtualClusterClient) *UserService {
	return &UserService{vcClient: vcClient}
}

func (s *UserService) Get(ctx context.Context, identifier string, includeJobs bool) ([]*UserGetResult, error) {
	return s.get(ctx, identifier, includeJobs, UserWorkloadOptions{})
}

func (s *UserService) GetWithWorkloads(ctx context.Context, identifier string, options UserWorkloadOptions) ([]*UserGetResult, error) {
	return s.get(ctx, identifier, false, options)
}

func (s *UserService) get(ctx context.Context, identifier string, includeJobs bool, workloadOptions UserWorkloadOptions) ([]*UserGetResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for user lookup")
	}

	users, err := s.vcClient.FindUsers(ctx, identifier)
	if err != nil {
		return nil, err
	}

	var jobList []JobClusterItem
	if includeJobs {
		submitters := userSubmitters(users)
		jobService := NewJobService(nil, nil, s.vcClient)
		jobList, err = jobService.GetCurrentTenantUserJobs(ctx, submitters)
		if err != nil {
			return nil, err
		}
	}

	groupsByUser := make(map[string][]platform.IAMGroup, len(users))
	for _, user := range users {
		groups, err := s.vcClient.ListUserGroups(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("list groups for user %q: %w", firstNonEmpty(user.Username, user.ID), err)
		}
		groupsByUser[user.ID] = groups
	}

	var workloads []UserWorkloadItem
	var workloadWarnings []string
	if workloadOptions.Any() {
		workloads, workloadWarnings, err = s.listUserWorkloads(ctx, users, workloadOptions)
		if err != nil {
			return nil, err
		}
	}

	results := make([]*UserGetResult, 0, len(users))
	for _, user := range users {
		submittedJobs := make([]JobClusterItem, 0)
		if includeJobs {
			for _, item := range jobList {
				submitter := strings.TrimSpace(item.Submitter)
				if submitter == "" {
					continue
				}
				if submitter == strings.TrimSpace(user.Username) || submitter == strings.TrimSpace(user.Name) {
					submittedJobs = append(submittedJobs, item)
				}
			}
		}
		results = append(results, &UserGetResult{
			ID:               user.ID,
			Username:         user.Username,
			Name:             user.Name,
			TenantCode:       user.TenantCode,
			Status:           user.Status,
			Source:           user.Source,
			Groups:           userGroupItems(groupsByUser[user.ID]),
			Jobs:             submittedJobs,
			Workloads:        filterUserWorkloads(workloads, userIdentitySet([]platform.IAMUser{user})),
			WorkloadWarnings: append([]string(nil), workloadWarnings...),
		})
	}
	return results, nil
}

func (s *UserService) listUserWorkloads(ctx context.Context, users []platform.IAMUser, options UserWorkloadOptions) ([]UserWorkloadItem, []string, error) {
	types := make([]string, 0, 4)
	if options.AID {
		types = append(types, "AID")
	}
	if options.AIT {
		types = append(types, "AIT")
	}
	if options.ATE {
		types = append(types, "ATE")
	}
	if options.AIR {
		types = append(types, "AIR")
	}

	identities := userIdentitySet(users)
	allWorkspaces := []platform.SSPWorkspace(nil)
	if options.AID || options.AIT || options.AIR {
		workspaces, err := s.vcClient.ListSSPWorkspaces(ctx, "")
		if err != nil {
			return nil, nil, fmt.Errorf("list SSP workspaces: %w", err)
		}
		allWorkspaces = workspaces
	}
	type workloadLoad struct {
		kind  string
		items []UserWorkloadItem
		err   error
	}
	loads := boundedMap(ctx, types, 4, func(ctx context.Context, kind string) workloadLoad {
		switch kind {
		case "AID":
			items, err := s.listAIDUserWorkloads(ctx, allWorkspaces, identities)
			if err != nil {
				return workloadLoad{kind: kind, err: err}
			}
			return workloadLoad{kind: kind, items: items}
		case "AIT":
			items, err := s.listAITUserWorkloads(ctx, allWorkspaces, users, identities)
			if err != nil {
				return workloadLoad{kind: kind, err: err}
			}
			return workloadLoad{kind: kind, items: items}
		case "ATE":
			jobs, err := NewJobService(nil, nil, s.vcClient).GetCurrentTenantUserJobsWithStatus(ctx, userSubmitters(users), options.IncludeInactiveATE)
			if err != nil {
				return workloadLoad{kind: kind, err: err}
			}
			items := make([]UserWorkloadItem, 0, len(jobs))
			for _, job := range jobs {
				items = append(items, UserWorkloadItem{
					Type: kind, Name: job.JobName, State: job.Status, VCluster: job.ClusterName,
					Creator: job.Submitter, CreatedAt: job.CreatedAt, createdTime: job.CreatedAtTime,
				})
			}
			return workloadLoad{kind: kind, items: items}
		case "AIR":
			items, err := s.listAIRUserWorkloads(ctx, allWorkspaces, identities)
			if err != nil {
				return workloadLoad{kind: kind, err: err}
			}
			return workloadLoad{kind: kind, items: items}
		default:
			return workloadLoad{kind: kind, err: fmt.Errorf("unsupported workload type %s", kind)}
		}
	})

	items := make([]UserWorkloadItem, 0)
	warnings := make([]string, 0)
	errs := make([]error, 0)
	for _, load := range loads {
		if load.err != nil {
			errs = append(errs, fmt.Errorf("query %s workloads: %w", load.kind, load.err))
			warnings = append(warnings, fmt.Sprintf("%s workload 查询失败：%v", load.kind, load.err))
			continue
		}
		items = append(items, load.items...)
	}
	if len(errs) == len(loads) && len(loads) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].createdTime.Equal(items[j].createdTime) {
			return items[i].createdTime.After(items[j].createdTime)
		}
		if items[i].Type != items[j].Type {
			return items[i].Type < items[j].Type
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, warnings, nil
}

func (s *UserService) listAITUserWorkloads(ctx context.Context, allWorkspaces []platform.SSPWorkspace, users []platform.IAMUser, identities map[string]struct{}) ([]UserWorkloadItem, error) {
	type target struct {
		workspace platform.SSPWorkspace
		creatorID string
	}
	creatorIDs := userCreatorIDs(users)
	targetCapacity := len(allWorkspaces)
	if len(creatorIDs) > 1 {
		targetCapacity *= len(creatorIDs)
	}
	targets := make([]target, 0, targetCapacity)
	for _, workspace := range allWorkspaces {
		if len(creatorIDs) == 0 {
			targets = append(targets, target{workspace: workspace})
			continue
		}
		for _, creatorID := range creatorIDs {
			targets = append(targets, target{workspace: workspace, creatorID: creatorID})
		}
	}
	type load struct {
		items []platform.SSPTrainingJob
		err   error
	}
	loads := boundedMap(ctx, targets, 12, func(ctx context.Context, target target) load {
		var items []platform.SSPTrainingJob
		var err error
		if target.creatorID == "" {
			items, err = s.vcClient.ListSSPTrainingJobsInWorkspaceSince(ctx, target.workspace, "", -1, time.Time{})
		} else {
			items, err = s.vcClient.ListSSPTrainingJobsForCreatorInWorkspace(ctx, target.workspace, target.creatorID, -1)
		}
		return load{items: items, err: err}
	})
	items := make([]UserWorkloadItem, 0)
	seen := make(map[string]struct{})
	var firstErr error
	succeeded := 0
	for _, result := range loads {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		succeeded++
		for _, job := range result.items {
			creator := firstNonEmpty(job.Ownership.CreatorName, job.Ownership.CreatorID)
			if !matchesUserIdentity(identities, creator, job.Ownership.CreatorID) {
				continue
			}
			key := strings.ToLower(strings.Join([]string{job.ProfileName, job.WorkspaceName, firstNonEmpty(job.UID, job.Name)}, "|"))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			createdAt := formatSSPTime(job.Status.CreateTime)
			items = append(items, UserWorkloadItem{
				Type: "AIT", Name: firstNonEmpty(job.Name, job.DisplayName), State: job.Status.State,
				Workspace: job.WorkspaceName, Queue: firstNonEmpty(job.Spec.Queue.Name, lastResourceSegment(job.Spec.Queue.ID)),
				Creator: creator, CreatorID: job.Ownership.CreatorID, CreatedAt: createdAt, createdTime: parseUserWorkloadTime(createdAt),
			})
		}
	}
	if succeeded == 0 && len(loads) > 0 {
		return nil, firstErr
	}
	return items, nil
}

func userCreatorIDs(users []platform.IAMUser) []string {
	result := make([]string, 0, len(users))
	seen := make(map[string]struct{}, len(users))
	for _, user := range users {
		value := strings.TrimSpace(user.ID)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func userWorkspaceKey(workspace platform.SSPWorkspace) string {
	return strings.ToLower(strings.Join([]string{workspace.ProfileName, workspace.Region, workspace.Name}, "|"))
}

func (s *UserService) listAIDUserWorkloads(ctx context.Context, workspaces []platform.SSPWorkspace, identities map[string]struct{}) ([]UserWorkloadItem, error) {
	type load struct {
		items []platform.SSPAID
		err   error
	}
	loads := boundedMap(ctx, workspaces, 12, func(ctx context.Context, workspace platform.SSPWorkspace) load {
		items, err := s.vcClient.ListSSPAIDsInWorkspaceSince(ctx, workspace, "", -1, time.Time{})
		return load{items: items, err: err}
	})
	items := make([]UserWorkloadItem, 0)
	var firstErr error
	succeeded := 0
	for _, result := range loads {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		succeeded++
		for _, aid := range result.items {
			creatorID := firstNonEmpty(aid.Properties.Ownership.CreatorID, aid.CreatorID)
			creator := firstNonEmpty(aid.Properties.Ownership.CreatorName, creatorID)
			if !matchesUserIdentity(identities, creator, creatorID) {
				continue
			}
			createdAt := formatSSPTime(aid.CreateTime)
			items = append(items, UserWorkloadItem{
				Type: "AID", Name: firstNonEmpty(aid.Name, aid.DisplayName), State: aid.State,
				Workspace: aid.Properties.Workload.WorkspaceName,
				Queue:     firstNonEmpty(aid.Properties.Workload.Queue.Name, lastResourceSegment(aid.Properties.Workload.Queue.ID)),
				Creator:   creator, CreatorID: creatorID, CreatedAt: createdAt, createdTime: parseUserWorkloadTime(createdAt),
			})
		}
	}
	if succeeded == 0 && len(loads) > 0 {
		return nil, firstErr
	}
	return items, nil
}

func (s *UserService) listAIRUserWorkloads(ctx context.Context, workspaces []platform.SSPWorkspace, identities map[string]struct{}) ([]UserWorkloadItem, error) {
	type load struct {
		items []platform.SSPAIRJob
		err   error
	}
	loads := boundedMap(ctx, workspaces, 12, func(ctx context.Context, workspace platform.SSPWorkspace) load {
		items, err := s.vcClient.ListSSPAIRJobs(ctx, workspace, "")
		return load{items: items, err: err}
	})
	airService := NewSSPAIRService(s.vcClient)
	items := make([]UserWorkloadItem, 0)
	var firstErr error
	succeeded := 0
	for _, result := range loads {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		succeeded++
		for _, job := range result.items {
			creator := firstNonEmpty(job.Ownership.CreatorName, job.Ownership.CreatorID)
			if !matchesUserIdentity(identities, creator, job.Ownership.CreatorID) {
				continue
			}
			item := airService.airJobItem(job)
			items = append(items, UserWorkloadItem{
				Type: "AIR", Name: item.Name, State: item.State, Workspace: item.Workspace,
				Queue: item.Queue, VCluster: item.Cluster, Creator: creator, CreatorID: job.Ownership.CreatorID,
				CreatedAt: item.CreatedAt, createdTime: parseUserWorkloadTime(item.CreatedAt),
			})
		}
	}
	if succeeded == 0 && len(loads) > 0 {
		return nil, firstErr
	}
	return items, nil
}

func userWorkspaces(workspaces []platform.SSPWorkspace, users []platform.IAMUser, groupsByUser map[string][]platform.IAMGroup) []platform.SSPWorkspace {
	groupSuffixes := make(map[string]struct{})
	for _, user := range users {
		for _, group := range groupsByUser[user.ID] {
			for _, value := range []string{group.Name, group.DisplayName, group.PosixGroupName} {
				value = strings.ToLower(strings.TrimSpace(value))
				if suffix := strings.TrimPrefix(value, "ug-"); suffix != "" && suffix != value {
					groupSuffixes[suffix] = struct{}{}
				}
			}
		}
	}
	if len(groupSuffixes) == 0 {
		return workspaces
	}
	matched := make([]platform.SSPWorkspace, 0)
	for _, workspace := range workspaces {
		suffix := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(workspace.Name)), "ws-")
		candidates := []string{suffix, strings.TrimPrefix(suffix, "d-"), strings.TrimPrefix(suffix, "p-"), strings.TrimPrefix(suffix, "pt-")}
		for _, candidate := range candidates {
			if _, ok := groupSuffixes[candidate]; ok {
				matched = append(matched, workspace)
				break
			}
		}
	}
	if len(matched) == 0 {
		return workspaces
	}
	return matched
}

func userSubmitters(users []platform.IAMUser) []string {
	values := make([]string, 0, len(users))
	for _, user := range users {
		if username := strings.TrimSpace(user.Username); username != "" {
			values = append(values, username)
		}
	}
	return values
}

func userIdentitySet(users []platform.IAMUser) map[string]struct{} {
	identities := make(map[string]struct{}, len(users)*3)
	for _, user := range users {
		for _, value := range []string{user.ID, user.Username, user.Name} {
			if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
				identities[value] = struct{}{}
			}
		}
	}
	return identities
}

func matchesUserIdentity(identities map[string]struct{}, values ...string) bool {
	for _, value := range values {
		if _, ok := identities[strings.ToLower(strings.TrimSpace(value))]; ok {
			return true
		}
	}
	return false
}

func filterUserWorkloads(items []UserWorkloadItem, identities map[string]struct{}) []UserWorkloadItem {
	result := make([]UserWorkloadItem, 0, len(items))
	for _, item := range items {
		if matchesUserIdentity(identities, item.Creator, item.CreatorID) {
			result = append(result, item)
		}
	}
	return result
}

func parseUserWorkloadTime(value string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(value), utcPlus8); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func userGroupItems(groups []platform.IAMGroup) []AuthGroupItem {
	items := make([]AuthGroupItem, 0, len(groups))
	for _, group := range groups {
		items = append(items, AuthGroupItem{
			ID:             strings.TrimSpace(group.ID),
			Name:           strings.TrimSpace(group.Name),
			DisplayName:    strings.TrimSpace(group.DisplayName),
			PosixGroupName: strings.TrimSpace(group.PosixGroupName),
			Status:         strings.TrimSpace(group.Status),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := firstNonEmpty(items[i].DisplayName, items[i].Name, items[i].PosixGroupName, items[i].ID)
		right := firstNonEmpty(items[j].DisplayName, items[j].Name, items[j].PosixGroupName, items[j].ID)
		if left != right {
			return left < right
		}
		return items[i].ID < items[j].ID
	})
	return items
}
