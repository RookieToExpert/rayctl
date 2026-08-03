package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"rayctl/internal/platform"
)

type UserGetResult struct {
	ID         string
	Username   string
	Name       string
	TenantCode string
	Status     string
	Source     string
	Groups     []AuthGroupItem
	Jobs       []JobClusterItem
}

type UserService struct {
	vcClient *platform.VirtualClusterClient
}

func NewUserService(vcClient *platform.VirtualClusterClient) *UserService {
	return &UserService{vcClient: vcClient}
}

func (s *UserService) Get(ctx context.Context, identifier string, includeJobs bool) ([]*UserGetResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for user lookup")
	}

	users, err := s.vcClient.FindUsers(ctx, identifier)
	if err != nil {
		return nil, err
	}

	var jobList []JobClusterItem
	if includeJobs {
		submitters := make([]string, 0, len(users))
		for _, user := range users {
			if username := strings.TrimSpace(user.Username); username != "" {
				submitters = append(submitters, username)
			}
		}
		jobService := NewJobService(nil, nil, s.vcClient)
		jobList, err = jobService.GetCurrentTenantUserJobs(ctx, submitters)
		if err != nil {
			return nil, err
		}
	}

	results := make([]*UserGetResult, 0, len(users))
	for _, user := range users {
		groups, err := s.vcClient.ListUserGroups(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("list groups for user %q: %w", firstNonEmpty(user.Username, user.ID), err)
		}
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
			ID:         user.ID,
			Username:   user.Username,
			Name:       user.Name,
			TenantCode: user.TenantCode,
			Status:     user.Status,
			Source:     user.Source,
			Groups:     userGroupItems(groups),
			Jobs:       submittedJobs,
		})
	}
	return results, nil
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
