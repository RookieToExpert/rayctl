package service

import (
	"context"

	"rayctl/internal/platform"
)

type UserListItem struct {
	ID         string
	Username   string
	Name       string
	TenantCode string
	Status     string
	Source     string
	CreateTime string
}

type GroupListItem struct {
	ID             string
	Name           string
	DisplayName    string
	PosixGroupName string
	TenantID       string
	CreateTime     string
}

type DirectoryService struct {
	client *platform.VirtualClusterClient
}

func NewDirectoryService(client *platform.VirtualClusterClient) *DirectoryService {
	return &DirectoryService{client: client}
}

func (s *DirectoryService) ListUsers(ctx context.Context, profileName string, limit int) ([]UserListItem, error) {
	users, err := s.client.ListIAMUsersForProfile(ctx, profileName, limit)
	if err != nil {
		return nil, err
	}
	result := make([]UserListItem, 0, len(users))
	for _, user := range users {
		result = append(result, UserListItem{
			ID: user.ID, Username: user.Username, Name: user.Name,
			TenantCode: user.TenantCode, Status: user.Status, Source: user.Source,
			CreateTime: user.CreateTime,
		})
	}
	return result, nil
}

func (s *DirectoryService) ListGroups(ctx context.Context, profileName string, limit int) ([]GroupListItem, error) {
	groups, err := s.client.ListIAMGroupsForProfile(ctx, profileName, limit)
	if err != nil {
		return nil, err
	}
	result := make([]GroupListItem, 0, len(groups))
	for _, group := range groups {
		result = append(result, GroupListItem{
			ID: group.ID, Name: group.Name, DisplayName: group.DisplayName,
			PosixGroupName: group.PosixGroupName, TenantID: group.TenantID,
			CreateTime: group.CreateTime,
		})
	}
	return result, nil
}
