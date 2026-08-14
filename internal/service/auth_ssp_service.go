package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"rayctl/internal/platform"
)

type AuthSSPResult struct {
	Workspace   string
	Environment string
	ProfileName string
	Items       []AuthSSPMemberItem
}

type AuthSSPMemberItem struct {
	Type        string
	Name        string
	DisplayName string
	ID          string
	Priority    string
	Roles       string
	RoleNames   string
}

type AuthSSPGrantRequest struct {
	Workspace        string
	ProfileName      string
	Environment      string
	MemberType       string
	MemberIdentifier string
	Roles            []string
	Priority         string
	BearerToken      string
	DryRun           bool
}

type AuthSSPGrantResult struct {
	Workspace   string
	Environment string
	ProfileName string
	MemberType  string
	MemberName  string
	MemberID    string
	Priority    string
	Roles       string
	Result      string
	Payload     string
}

func (s *AuthService) GetSSPWorkspaceAuth(ctx context.Context, profileName string, environment string, workspaceName string, bearerToken string) (*AuthSSPResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for SSP auth lookup")
	}
	workspace, err := s.vcClient.FindSSPWorkspaceForProfile(ctx, profileName, workspaceName)
	if err != nil {
		return nil, err
	}
	members, err := s.vcClient.ListSSPWorkspaceMembers(ctx, *workspace, bearerToken)
	if err != nil {
		return nil, fmt.Errorf("list SSP workspace members: %w", err)
	}
	items := make([]AuthSSPMemberItem, 0, len(members))
	for _, member := range members {
		displays := make([]string, 0, len(member.Roles))
		names := make([]string, 0, len(member.Roles))
		for _, role := range member.Roles {
			displays = append(displays, firstNonEmpty(strings.TrimSpace(role.DisplayName), strings.TrimSpace(role.Name), strings.TrimSpace(role.ID)))
			if value := strings.TrimSpace(role.Name); value != "" {
				names = append(names, value)
			}
		}
		items = append(items, AuthSSPMemberItem{
			Type:        strings.ToUpper(strings.TrimSpace(member.Type)),
			Name:        strings.TrimSpace(member.Name),
			DisplayName: strings.TrimSpace(member.DisplayName),
			ID:          strings.TrimSpace(member.ID),
			Priority:    strings.ToUpper(strings.TrimSpace(member.AdvancedSettings.PriorityCap)),
			Roles:       strings.Join(displays, ", "),
			RoleNames:   strings.Join(names, ", "),
		})
	}
	return &AuthSSPResult{
		Workspace:   workspace.Name,
		Environment: environment,
		ProfileName: workspace.ProfileName,
		Items:       items,
	}, nil
}

func (s *AuthService) GrantSSPWorkspace(ctx context.Context, req AuthSSPGrantRequest) (*AuthSSPGrantResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for SSP auth grant")
	}
	workspace, err := s.vcClient.FindSSPWorkspaceForProfile(ctx, req.ProfileName, req.Workspace)
	if err != nil {
		return nil, err
	}
	memberType := strings.ToUpper(strings.TrimSpace(req.MemberType))
	if memberType != "USER" && memberType != "GROUP" {
		return nil, fmt.Errorf("member type must be USER or GROUP")
	}
	member, err := s.resolveSSPWorkspaceMember(ctx, workspace.ProfileName, memberType, req.MemberIdentifier, req.BearerToken)
	if err != nil {
		return nil, err
	}

	scope := sspWorkspaceScope(*workspace)
	roles, err := s.vcClient.ListOwnIAMRolesForProfile(ctx, workspace.ProfileName, scope, req.BearerToken)
	if err != nil {
		return nil, fmt.Errorf("list SSP workspace roles: %w", err)
	}
	selectedRoles, err := resolveSSPWorkspaceRoles(req.Roles, roles, scope)
	if err != nil {
		return nil, err
	}
	priority := strings.ToUpper(strings.TrimSpace(req.Priority))
	if priority == "" {
		priority = "NORMAL"
	}
	priorities, err := s.vcClient.ListSSPWorkspacePriorities(ctx, *workspace, req.BearerToken)
	if err != nil {
		return nil, fmt.Errorf("list SSP workspace priorities: %w", err)
	}
	if !containsFold(priorities, priority) {
		return nil, fmt.Errorf("unsupported priority %q; available values: %s", priority, strings.Join(priorities, ", "))
	}

	payloadMember := platform.SSPWorkspaceMember{
		Type:        memberType,
		ID:          strings.TrimSpace(member.ID),
		Name:        strings.TrimSpace(member.Name),
		DisplayName: strings.TrimSpace(member.DisplayName),
		Roles:       sspWorkspaceRolePayload(selectedRoles),
	}
	payloadMember.AdvancedSettings.PriorityCap = priority
	payload := platform.SSPWorkspaceAddMembersRequest{Members: []platform.SSPWorkspaceMember{payloadMember}}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	roleDisplays := make([]string, 0, len(selectedRoles))
	for _, role := range selectedRoles {
		roleDisplays = append(roleDisplays, firstNonEmpty(role.DisplayName, role.Name, role.ID))
	}
	result := &AuthSSPGrantResult{
		Workspace:   workspace.Name,
		Environment: req.Environment,
		ProfileName: workspace.ProfileName,
		MemberType:  memberType,
		MemberName:  firstNonEmpty(member.Name, member.DisplayName),
		MemberID:    member.ID,
		Priority:    priority,
		Roles:       strings.Join(roleDisplays, ", "),
		Result:      "dry-run",
		Payload:     string(payloadJSON),
	}
	if req.DryRun {
		return result, nil
	}
	if _, err := s.vcClient.AddSSPWorkspaceMembers(ctx, *workspace, payload, req.BearerToken); err != nil {
		return nil, fmt.Errorf("add SSP workspace member: %w", err)
	}
	result.Result = "updated"
	result.Payload = ""
	return result, nil
}

func sspWorkspaceRolePayload(roles []platform.SSPWorkspaceRole) []platform.SSPWorkspaceRole {
	result := make([]platform.SSPWorkspaceRole, 0, len(roles))
	for _, role := range roles {
		result = append(result, platform.SSPWorkspaceRole{ID: role.ID, Scope: role.Scope})
	}
	return result
}

func (s *AuthService) resolveSSPWorkspaceMember(ctx context.Context, profileName string, memberType string, identifier string, bearerToken string) (*platform.IAMUserGroupSearchItem, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("member identifier is required")
	}
	items, err := s.vcClient.SearchSSPMembersForProfile(ctx, profileName, identifier, bearerToken)
	if err != nil {
		return nil, fmt.Errorf("search SSP workspace member: %w", err)
	}
	matches := make([]platform.IAMUserGroupSearchItem, 0)
	for _, item := range items {
		itemType := strings.ToUpper(strings.TrimSpace(item.Type))
		if itemType == "USER_GROUP" {
			itemType = "GROUP"
		}
		if itemType != memberType {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(item.ID), identifier) ||
			strings.EqualFold(strings.TrimSpace(item.Name), identifier) ||
			strings.EqualFold(strings.TrimSpace(item.DisplayName), identifier) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%s %q not found in profile %q", strings.ToLower(memberType), identifier, profileName)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%s %q matched multiple members; please use member id", strings.ToLower(memberType), identifier)
	}
	return &matches[0], nil
}

func resolveSSPWorkspaceRoles(requested []string, available []platform.IAMRoleInfo, scope string) ([]platform.SSPWorkspaceRole, error) {
	values := make([]string, 0)
	for _, raw := range requested {
		for _, value := range strings.Split(raw, ",") {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one SSP role is required")
	}
	result := make([]platform.SSPWorkspaceRole, 0, len(values))
	seen := make(map[string]struct{})
	for _, requestedRole := range values {
		var matches []platform.IAMRoleInfo
		for _, role := range available {
			if sspRoleMatches(role, requestedRole) {
				matches = append(matches, role)
			}
		}
		if len(matches) == 0 {
			aliases := make([]string, 0, len(available))
			for _, role := range available {
				aliases = append(aliases, sspRoleAlias(role.RoleName))
			}
			sort.Strings(aliases)
			return nil, fmt.Errorf("SSP role %q not found; available aliases: %s", requestedRole, strings.Join(aliases, ", "))
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("SSP role %q is ambiguous; please use full role name", requestedRole)
		}
		role := matches[0]
		if _, ok := seen[role.ID]; ok {
			continue
		}
		seen[role.ID] = struct{}{}
		result = append(result, platform.SSPWorkspaceRole{
			ID:          role.ID,
			Scope:       scope,
			Name:        role.RoleName,
			DisplayName: role.DisplayName,
			Description: role.Description,
		})
	}
	return result, nil
}

func sspRoleMatches(role platform.IAMRoleInfo, requested string) bool {
	requested = normalizedSSPRoleKey(requested)
	return requested == normalizedSSPRoleKey(role.ID) ||
		requested == normalizedSSPRoleKey(role.RoleName) ||
		requested == normalizedSSPRoleKey(role.DisplayName) ||
		requested == normalizedSSPRoleKey(sspRoleAlias(role.RoleName))
}

func normalizedSSPRoleKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "ssp.")
	return strings.NewReplacer("-", "", "_", "", ".", "").Replace(value)
}

func sspRoleAlias(roleName string) string {
	roleName = strings.TrimSpace(strings.TrimPrefix(roleName, "ssp."))
	var result strings.Builder
	for i, r := range roleName {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('-')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

func sspWorkspaceScope(workspace platform.SSPWorkspace) string {
	return fmt.Sprintf(
		"/rm/subscriptions/%s/resourceGroups/%s/regions/%s/workspaces/%s",
		workspace.Subscription,
		firstNonEmpty(workspace.ResourceGroup, "default"),
		workspace.Region,
		workspace.Name,
	)
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
