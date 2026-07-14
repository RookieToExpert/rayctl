package service

import (
	"context"
	"fmt"
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
			Jobs:       submittedJobs,
		})
	}
	return results, nil
}
