package platform

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type SSPWorkspaceRole struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

type SSPWorkspaceMember struct {
	Type             string             `json:"type"`
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	DisplayName      string             `json:"display_name"`
	Roles            []SSPWorkspaceRole `json:"roles"`
	AdvancedSettings struct {
		PriorityCap string `json:"priority_cap"`
	} `json:"advanced_settings"`
}

type SSPWorkspaceMembersResponse struct {
	Members       []SSPWorkspaceMember `json:"members"`
	TotalSize     int                  `json:"total_size"`
	NextPageToken string               `json:"next_page_token"`
}

type SSPWorkspaceAddMembersRequest struct {
	Members []SSPWorkspaceMember `json:"members"`
}

type sspWorkspacePrioritiesResponse struct {
	Priorities []string `json:"priorities"`
}

func (c *VirtualClusterClient) FindSSPWorkspaceForProfile(ctx context.Context, profileName string, workspaceName string) (*SSPWorkspace, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	workspaceName = strings.TrimSpace(workspaceName)
	if workspaceName == "" {
		return nil, fmt.Errorf("workspace name is required")
	}

	pageToken := "1"
	for {
		endpoint, _ := url.Parse(profile.BaseURL)
		endpoint.Path = "/rmh/v1/resources:page"
		query := endpoint.Query()
		query.Set("filter", fmt.Sprintf(`resource_type="compute.ssp.v1.workspace" AND name="%s"`, escapeSSPFilterValue(workspaceName)))
		query.Set("page_size", "100")
		query.Set("page_token", pageToken)
		endpoint.RawQuery = query.Encode()

		var payload storageVolumePageResponse
		if err := c.postJSONWithProfile(ctx, profile, endpoint.String(), map[string]any{}, &payload); err != nil {
			return nil, err
		}
		for _, resource := range payload.Resources {
			if strings.EqualFold(strings.TrimSpace(resource.Name), workspaceName) {
				workspace := sspWorkspaceFromResource(resource, profile.Name)
				if workspace.Subscription == "" {
					workspace.Subscription = strings.TrimSpace(profile.Subscription)
				}
				if workspace.ResourceGroup == "" {
					workspace.ResourceGroup = firstNonEmpty(strings.TrimSpace(profile.ResourceGroup), defaultResourceGroup)
				}
				if workspace.Region == "" {
					workspace.Region = strings.TrimSpace(profile.Region)
				}
				return &workspace, nil
			}
		}
		next := strings.TrimSpace(payload.NextPageToken)
		if next == "" || next == pageToken || len(payload.Resources) == 0 {
			break
		}
		pageToken = next
	}
	return nil, fmt.Errorf("SSP workspace %q not found in profile %q", workspaceName, profile.Name)
}

func (c *VirtualClusterClient) ListOwnIAMRolesForProfile(ctx context.Context, profileName string, scope string, bearerToken string) ([]IAMRoleInfo, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil, fmt.Errorf("scope is required")
	}

	endpoint, _ := url.Parse(profile.IAMBaseURL)
	endpoint.Path = "/iam/authz/v1/roles:own"
	query := endpoint.Query()
	query.Set("page_size", "100")
	query.Set("page_token", "1")
	query.Set("scope", scope)
	query.Set("level", "resources")
	endpoint.RawQuery = query.Encode()

	var payload iamRoleListResponse
	if err := c.getJSONWithBearerProfile(ctx, profile, endpoint.String(), bearerToken, &payload); err != nil {
		return nil, err
	}
	roles := payload.Roles
	if len(roles) == 0 {
		roles = payload.RoleInfos
	}
	if len(roles) == 0 {
		roles = payload.Items
	}
	return roles, nil
}

func (c *VirtualClusterClient) SearchSSPMembersForProfile(ctx context.Context, profileName string, searchQuery string, bearerToken string) ([]IAMUserGroupSearchItem, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	searchQuery = strings.TrimSpace(searchQuery)
	if searchQuery == "" {
		return nil, fmt.Errorf("member search query is required")
	}

	endpoint, _ := url.Parse(profile.IAMBaseURL)
	endpoint.Path = "/iam/idp/v1/users/searchUsersAndGroups"
	query := endpoint.Query()
	query.Set("query", searchQuery)
	query.Set("page_size", "100")
	endpoint.RawQuery = query.Encode()

	var payload iamUserGroupSearchResponse
	if err := c.getJSONWithBearerProfile(ctx, profile, endpoint.String(), bearerToken, &payload); err != nil {
		return nil, err
	}
	items := append([]IAMUserGroupSearchItem{}, payload.Items...)
	items = append(items, payload.Users...)
	items = append(items, payload.Groups...)
	items = append(items, payload.UserGroups...)
	return items, nil
}

func (c *VirtualClusterClient) ListSSPWorkspacePriorities(ctx context.Context, workspace SSPWorkspace, bearerToken string) ([]string, error) {
	profile, endpoint, err := c.sspWorkspaceEndpoint(workspace, "/priorities")
	if err != nil {
		return nil, err
	}
	var payload sspWorkspacePrioritiesResponse
	if err := c.getJSONWithBearerProfile(ctx, profile, endpoint, bearerToken, &payload); err != nil {
		return nil, err
	}
	return payload.Priorities, nil
}

func (c *VirtualClusterClient) ListSSPWorkspaceMembers(ctx context.Context, workspace SSPWorkspace, bearerToken string) ([]SSPWorkspaceMember, error) {
	profile, endpoint, err := c.sspWorkspaceEndpoint(workspace, "/members")
	if err != nil {
		return nil, err
	}
	result := make([]SSPWorkspaceMember, 0)
	for skip := 0; ; {
		u, _ := url.Parse(endpoint)
		query := u.Query()
		query.Set("page_size", "100")
		query.Set("skip", fmt.Sprintf("%d", skip))
		u.RawQuery = query.Encode()
		var payload SSPWorkspaceMembersResponse
		if err := c.getJSONWithBearerProfile(ctx, profile, u.String(), bearerToken, &payload); err != nil {
			return nil, err
		}
		result = append(result, payload.Members...)
		if len(payload.Members) == 0 || skip+len(payload.Members) >= payload.TotalSize {
			break
		}
		skip += len(payload.Members)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (c *VirtualClusterClient) AddSSPWorkspaceMembers(ctx context.Context, workspace SSPWorkspace, payload SSPWorkspaceAddMembersRequest, bearerToken string) ([]SSPWorkspaceMember, error) {
	profile, endpoint, err := c.sspWorkspaceEndpoint(workspace, "/members:add")
	if err != nil {
		return nil, err
	}
	var response SSPWorkspaceMembersResponse
	if err := c.postJSONWithBearerProfile(ctx, profile, endpoint, payload, bearerToken, &response); err != nil {
		return nil, err
	}
	return response.Members, nil
}

func (c *VirtualClusterClient) sspWorkspaceEndpoint(workspace SSPWorkspace, suffix string) (clientProfile, string, error) {
	profile, ok := c.clientProfileByName(workspace.ProfileName)
	if !ok {
		return clientProfile{}, "", fmt.Errorf("platform profile %q not found", workspace.ProfileName)
	}
	if strings.TrimSpace(workspace.Subscription) == "" || strings.TrimSpace(workspace.Region) == "" || strings.TrimSpace(workspace.Name) == "" {
		return clientProfile{}, "", fmt.Errorf("workspace subscription, region and name are required")
	}
	resourceGroup := firstNonEmpty(strings.TrimSpace(workspace.ResourceGroup), strings.TrimSpace(profile.ResourceGroup), defaultResourceGroup)
	endpoint := fmt.Sprintf(
		"%s/compute/ssp/v1/subscriptions/%s/resourceGroups/%s/regions/%s/workspaces/%s%s",
		strings.TrimRight(profile.BaseURL, "/"),
		url.PathEscape(workspace.Subscription),
		url.PathEscape(resourceGroup),
		url.PathEscape(workspace.Region),
		url.PathEscape(workspace.Name),
		suffix,
	)
	return profile, endpoint, nil
}
