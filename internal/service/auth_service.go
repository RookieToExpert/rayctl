package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"rayctl/internal/platform"
)

type AuthService struct {
	vcClient *platform.VirtualClusterClient
}

type AuthAFSResult struct {
	AFSName string
	Items   []AuthAFSItem
}

type AuthAFSItem struct {
	MemberType     string
	MemberName     string
	MemberIdentify string
	MemberValue    string
	Roles          string
	RoleNames      string
	PolicyID       string
	CreateTime     string
}

type AuthUserResult struct {
	ID          string
	Username    string
	Name        string
	TenantCode  string
	Status      string
	Source      string
	Groups      []AuthGroupItem
	Permissions []AuthPermissionItem
}

type AuthGroupResult struct {
	ID             string
	Name           string
	DisplayName    string
	PosixGroupName string
	TenantCode     string
	Status         string
	Permissions    []AuthPermissionItem
}

type AuthGroupItem struct {
	ID             string
	Name           string
	DisplayName    string
	PosixGroupName string
	Status         string
}

type AuthPermissionItem struct {
	Source     string
	Member     string
	Service    string
	Scope      string
	Roles      string
	RoleNames  string
	PolicyID   string
	CreateTime string
}

func NewAuthService(vcClient *platform.VirtualClusterClient) *AuthService {
	return &AuthService{vcClient: vcClient}
}

func (s *AuthService) GetAFS(ctx context.Context, afsName string) (*AuthAFSResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for auth lookup")
	}
	afsName = strings.TrimSpace(afsName)
	if afsName == "" {
		return nil, fmt.Errorf("afs name is required")
	}

	policies, err := s.vcClient.ListIAMBindingPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list binding policies: %w", err)
	}

	items := make([]AuthAFSItem, 0)
	for _, policy := range policies {
		if extractAFSNameFromScope(policy.Scope) != afsName {
			continue
		}
		roleDisplays, roleNames := extractIAMRoleNames(policy.RoleInfos)
		items = append(items, AuthAFSItem{
			MemberType:     strings.TrimSpace(policy.MemberType),
			MemberName:     strings.TrimSpace(policy.MemberName),
			MemberIdentify: strings.TrimSpace(policy.MemberIdentify),
			MemberValue:    strings.TrimSpace(policy.MemberValue),
			Roles:          strings.Join(roleDisplays, ","),
			RoleNames:      strings.Join(roleNames, ","),
			PolicyID:       strings.TrimSpace(policy.ID),
			CreateTime:     formatLocalTime(policy.CreateTime),
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].MemberType != items[j].MemberType {
			return items[i].MemberType < items[j].MemberType
		}
		if items[i].MemberIdentify != items[j].MemberIdentify {
			return items[i].MemberIdentify < items[j].MemberIdentify
		}
		if items[i].Roles != items[j].Roles {
			return items[i].Roles < items[j].Roles
		}
		return items[i].PolicyID < items[j].PolicyID
	})

	return &AuthAFSResult{
		AFSName: afsName,
		Items:   items,
	}, nil
}

func (s *AuthService) GetUser(ctx context.Context, identifier string) ([]*AuthUserResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for auth lookup")
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("user identifier is required")
	}

	users, err := s.vcClient.FindUsers(ctx, identifier)
	if err != nil {
		return nil, err
	}
	policies, err := s.vcClient.ListIAMBindingPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list binding policies: %w", err)
	}

	results := make([]*AuthUserResult, 0, len(users))
	for _, user := range users {
		groups, err := s.vcClient.ListUserGroups(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("list groups for user %q: %w", firstNonEmpty(user.Username, user.ID), err)
		}

		groupItems := make([]AuthGroupItem, 0, len(groups))
		groupByID := make(map[string]AuthGroupItem, len(groups))
		for _, group := range groups {
			item := AuthGroupItem{
				ID:             strings.TrimSpace(group.ID),
				Name:           strings.TrimSpace(group.Name),
				DisplayName:    strings.TrimSpace(group.DisplayName),
				PosixGroupName: strings.TrimSpace(group.PosixGroupName),
				Status:         strings.TrimSpace(group.Status),
			}
			groupItems = append(groupItems, item)
			if item.ID != "" {
				groupByID[item.ID] = item
			}
		}
		sort.SliceStable(groupItems, func(i, j int) bool {
			if groupDisplayName(groupItems[i]) != groupDisplayName(groupItems[j]) {
				return groupDisplayName(groupItems[i]) < groupDisplayName(groupItems[j])
			}
			return groupItems[i].ID < groupItems[j].ID
		})

		permissions := collectUserPermissions(user, groupByID, policies)
		results = append(results, &AuthUserResult{
			ID:          user.ID,
			Username:    user.Username,
			Name:        user.Name,
			TenantCode:  user.TenantCode,
			Status:      user.Status,
			Source:      user.Source,
			Groups:      groupItems,
			Permissions: permissions,
		})
	}
	return results, nil
}

func (s *AuthService) GetGroups(ctx context.Context, identifier string) ([]*AuthGroupResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for auth lookup")
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("group identifier is required")
	}

	groups, err := s.vcClient.FindGroups(ctx, identifier)
	if err != nil {
		return nil, err
	}
	policies, err := s.vcClient.ListIAMBindingPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list binding policies: %w", err)
	}

	results := make([]*AuthGroupResult, 0, len(groups))
	for _, group := range groups {
		item := AuthGroupItem{
			ID:             strings.TrimSpace(group.ID),
			Name:           strings.TrimSpace(group.Name),
			DisplayName:    strings.TrimSpace(group.DisplayName),
			PosixGroupName: strings.TrimSpace(group.PosixGroupName),
			Status:         strings.TrimSpace(group.Status),
		}
		results = append(results, &AuthGroupResult{
			ID:             item.ID,
			Name:           item.Name,
			DisplayName:    item.DisplayName,
			PosixGroupName: item.PosixGroupName,
			TenantCode:     strings.TrimSpace(group.TenantCode),
			Status:         item.Status,
			Permissions:    collectGroupPermissions(item, policies),
		})
	}
	return results, nil
}

func extractAFSNameFromScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}
	marker := "/virtualVolumes/"
	index := strings.Index(scope, marker)
	if index < 0 {
		return ""
	}
	name := scope[index+len(marker):]
	if cut := strings.IndexAny(name, "/?#"); cut >= 0 {
		name = name[:cut]
	}
	return strings.TrimSpace(name)
}

func collectUserPermissions(user platform.IAMUser, groupByID map[string]AuthGroupItem, policies []platform.IAMBindingPolicy) []AuthPermissionItem {
	userID := strings.TrimSpace(user.ID)
	items := make([]AuthPermissionItem, 0)
	seen := make(map[string]struct{})
	for _, policy := range policies {
		memberValue := strings.TrimSpace(policy.MemberValue)
		source := ""
		member := ""
		switch {
		case isIAMUserMember(policy.MemberType) && memberValue == userID:
			source = "DIRECT"
			member = firstNonEmpty(user.Username, user.Name, user.ID)
		case isIAMGroupMember(policy.MemberType):
			group, ok := groupByID[memberValue]
			if !ok {
				continue
			}
			source = "GROUP"
			member = groupDisplayName(group)
		default:
			continue
		}

		roleDisplays, roleNames := extractIAMRoleNames(policy.RoleInfos)
		item := AuthPermissionItem{
			Source:     source,
			Member:     member,
			Service:    strings.TrimSpace(policy.Service),
			Scope:      normalizeAuthScope(policy.Scope),
			Roles:      strings.Join(roleDisplays, ","),
			RoleNames:  strings.Join(roleNames, ","),
			PolicyID:   strings.TrimSpace(policy.ID),
			CreateTime: formatLocalTime(policy.CreateTime),
		}
		key := item.Source + "|" + item.Member + "|" + item.Scope + "|" + item.RoleNames + "|" + item.PolicyID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Source != items[j].Source {
			return items[i].Source < items[j].Source
		}
		if items[i].Service != items[j].Service {
			return items[i].Service < items[j].Service
		}
		if items[i].Scope != items[j].Scope {
			return items[i].Scope < items[j].Scope
		}
		if items[i].Member != items[j].Member {
			return items[i].Member < items[j].Member
		}
		return items[i].PolicyID < items[j].PolicyID
	})
	return items
}

func collectGroupPermissions(group AuthGroupItem, policies []platform.IAMBindingPolicy) []AuthPermissionItem {
	items := make([]AuthPermissionItem, 0)
	seen := make(map[string]struct{})
	for _, policy := range policies {
		if !isIAMGroupMember(policy.MemberType) {
			continue
		}
		if !policyMatchesGroup(policy, group) {
			continue
		}
		roleDisplays, roleNames := extractIAMRoleNames(policy.RoleInfos)
		item := AuthPermissionItem{
			Source:     "GROUP",
			Member:     groupDisplayName(group),
			Service:    strings.TrimSpace(policy.Service),
			Scope:      normalizeAuthScope(policy.Scope),
			Roles:      strings.Join(roleDisplays, ","),
			RoleNames:  strings.Join(roleNames, ","),
			PolicyID:   strings.TrimSpace(policy.ID),
			CreateTime: formatLocalTime(policy.CreateTime),
		}
		key := item.Member + "|" + item.Scope + "|" + item.RoleNames + "|" + item.PolicyID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Service != items[j].Service {
			return items[i].Service < items[j].Service
		}
		if items[i].Scope != items[j].Scope {
			return items[i].Scope < items[j].Scope
		}
		if items[i].RoleNames != items[j].RoleNames {
			return items[i].RoleNames < items[j].RoleNames
		}
		return items[i].PolicyID < items[j].PolicyID
	})
	return items
}

func policyMatchesGroup(policy platform.IAMBindingPolicy, group AuthGroupItem) bool {
	candidates := []string{
		group.ID,
		group.Name,
		group.DisplayName,
		group.PosixGroupName,
	}
	values := []string{
		policy.MemberValue,
		policy.MemberName,
		policy.MemberIdentify,
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for _, value := range values {
			if strings.TrimSpace(value) == candidate {
				return true
			}
		}
	}
	return false
}

func groupDisplayName(group AuthGroupItem) string {
	return firstNonEmpty(group.DisplayName, group.Name, group.PosixGroupName, group.ID)
}

func isIAMUserMember(memberType string) bool {
	return strings.EqualFold(strings.TrimSpace(memberType), "USER")
}

func isIAMGroupMember(memberType string) bool {
	switch strings.ToUpper(strings.TrimSpace(memberType)) {
	case "GROUP", "USER_GROUP", "USERGROUP", "GROUPS":
		return true
	default:
		return false
	}
}

func normalizeAuthScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if strings.HasPrefix(scope, "/rm/") {
		return strings.TrimPrefix(scope, "/rm")
	}
	return scope
}

func extractIAMRoleNames(roles []platform.IAMRoleInfo) ([]string, []string) {
	displays := make([]string, 0, len(roles))
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		if display := strings.TrimSpace(role.DisplayName); display != "" {
			displays = append(displays, display)
		}
		if name := strings.TrimSpace(role.RoleName); name != "" {
			names = append(names, name)
		}
	}
	return displays, names
}

func formatLocalTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return t.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
}
