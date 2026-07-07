package service

import (
	"context"
	"encoding/json"
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
	ResourceType string
	ResourceName string
	Scope        string
	AFSName      string
	Items        []AuthAFSItem
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

type AuthGrantAFSRequest struct {
	ResourceType     string
	ResourceName     string
	Scope            string
	MemberType       string
	MemberIdentifier string
	Role             string
	DryRun           bool
	BearerToken      string
}

type AuthGrantAFSResult struct {
	ResourceType   string
	ResourceName   string
	AFSName        string
	Scope          string
	MemberType     string
	MemberName     string
	MemberIdentify string
	MemberValue    string
	RoleName       string
	RoleID         string
	Result         string
	PolicyID       string
	Payload        string
}

type AuthRolesResult struct {
	ResourceType string
	ResourceName string
	Scope        string
	Items        []AuthRoleItem
}

type AuthRoleItem struct {
	Alias       string
	RoleName    string
	DisplayName string
	Description string
	RoleID      string
	Service     string
}

type authRoleDefinition struct {
	Alias    string
	RoleName string
}

func NewAuthService(vcClient *platform.VirtualClusterClient) *AuthService {
	return &AuthService{vcClient: vcClient}
}

func (s *AuthService) GetAFS(ctx context.Context, afsName string) (*AuthAFSResult, error) {
	return s.GetResourceAuth(ctx, "afs", afsName)
}

func (s *AuthService) GetResourceAuth(ctx context.Context, resourceType string, resourceName string) (*AuthAFSResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for auth lookup")
	}
	resourceType = normalizeGrantResourceType(resourceType)
	if resourceType == "" {
		resourceType = "afs"
	}
	resourceName = strings.TrimSpace(resourceName)
	if resourceName == "" {
		return nil, fmt.Errorf("%s name is required", resourceType)
	}

	resource, err := s.findGrantResource(ctx, resourceType, resourceName)
	if err != nil {
		return nil, fmt.Errorf("resolve %s scope: %w", resourceType, err)
	}
	scope := ensureRMScope(resource.RID)
	if scope == "" {
		return nil, fmt.Errorf("%s scope is empty", resourceType)
	}
	resourceName = firstNonEmpty(strings.TrimSpace(resource.Name), resourceName)

	policies, err := s.vcClient.ListIAMBindingPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list binding policies: %w", err)
	}

	items := make([]AuthAFSItem, 0)
	for _, policy := range policies {
		if normalizeAuthScope(policy.Scope) != normalizeAuthScope(scope) && ensureRMScope(policy.Scope) != scope {
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
		ResourceType: strings.ToUpper(resourceType),
		ResourceName: resourceName,
		Scope:        scope,
		AFSName:      resourceName,
		Items:        items,
	}, nil
}

func (s *AuthService) GetResourceRoles(ctx context.Context, resourceType string) (*AuthRolesResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for auth roles")
	}
	resourceType = normalizeGrantResourceType(resourceType)
	if resourceType == "" {
		resourceType = "afs"
	}
	defs := grantRoleDefinitions(resourceType)
	if len(defs) == 0 {
		return nil, fmt.Errorf("unsupported resource type %q", resourceType)
	}
	roles, err := s.vcClient.ListIAMRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	roleByName := make(map[string]platform.IAMRoleInfo, len(roles))
	for _, role := range roles {
		roleName := strings.TrimSpace(role.RoleName)
		if roleName == "" {
			continue
		}
		roleByName[roleName] = role
	}
	items := make([]AuthRoleItem, 0, len(defs))
	for _, def := range defs {
		role := roleByName[def.RoleName]
		items = append(items, AuthRoleItem{
			Alias:       def.Alias,
			RoleName:    def.RoleName,
			DisplayName: strings.TrimSpace(role.DisplayName),
			Description: strings.TrimSpace(role.Description),
			RoleID:      strings.TrimSpace(role.ID),
			Service:     strings.TrimSpace(role.AvailableService),
		})
	}
	return &AuthRolesResult{
		ResourceType: strings.ToUpper(resourceType),
		Items:        items,
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

func (s *AuthService) GrantAFS(ctx context.Context, req AuthGrantAFSRequest) (*AuthGrantAFSResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for auth grant")
	}
	resourceType := normalizeGrantResourceType(req.ResourceType)
	if resourceType == "" {
		resourceType = "afs"
	}
	resourceName := strings.TrimSpace(req.ResourceName)
	scope := strings.TrimSpace(req.Scope)
	if resourceName == "" && scope == "" {
		return nil, fmt.Errorf("%s name or scope is required", resourceType)
	}

	memberType := strings.ToUpper(strings.TrimSpace(req.MemberType))
	if memberType != "USER" && memberType != "GROUP" {
		return nil, fmt.Errorf("member type must be USER or GROUP")
	}
	memberName, memberIdentify, memberValue, err := s.resolveGrantMember(ctx, memberType, req.MemberIdentifier)
	if err != nil {
		return nil, err
	}

	if scope == "" {
		resource, err := s.findGrantResource(ctx, resourceType, resourceName)
		if err != nil {
			return nil, fmt.Errorf("resolve %s scope: %w", resourceType, err)
		}
		scope = ensureRMScope(resource.RID)
		if resourceName == "" {
			resourceName = strings.TrimSpace(resource.Name)
		}
	}
	if resourceName == "" {
		resourceName = extractResourceNameFromScope(scope)
	}
	if scope == "" {
		return nil, fmt.Errorf("%s scope is empty", resourceType)
	}

	roleName, roleID, err := s.resolveGrantRole(ctx, resourceType, req.Role)
	if err != nil {
		return nil, err
	}

	result := &AuthGrantAFSResult{
		ResourceType:   strings.ToUpper(resourceType),
		ResourceName:   resourceName,
		AFSName:        resourceName,
		Scope:          scope,
		MemberType:     memberType,
		MemberName:     memberName,
		MemberIdentify: memberIdentify,
		MemberValue:    memberValue,
		RoleName:       roleName,
		RoleID:         roleID,
	}

	policies, err := s.vcClient.ListIAMBindingPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list binding policies: %w", err)
	}
	if policyID := existingGrantPolicyID(policies, scope, memberType, memberValue, roleName); policyID != "" {
		result.Result = "already exists"
		result.PolicyID = policyID
		return result, nil
	}

	payload := platform.IAMBatchCreatePoliciesRequest{
		Policies: []platform.IAMBatchCreatePolicy{
			{
				Scope:   scope,
				RoleIDs: []string{roleID},
				Members: []platform.IAMBatchCreateMember{
					{
						MemberType:  memberType,
						MemberValue: memberValue,
					},
				},
				ExcludeMembers: []platform.IAMBatchCreateMember{},
				Level:          "resources",
			},
		},
	}
	payloadBytes, _ := json.MarshalIndent(payload, "", "  ")
	result.Payload = string(payloadBytes)
	if req.DryRun {
		result.Result = "dry-run"
		return result, nil
	}

	resp, err := s.vcClient.BatchCreateIAMPolicies(ctx, payload, req.BearerToken)
	if err != nil {
		if strings.Contains(err.Error(), "novaoperation.operationService.auth") {
			if strings.TrimSpace(req.BearerToken) != "" {
				return nil, fmt.Errorf("set user policy denied: 当前控制台登录态缺少 novaoperation.operationService.auth，或 Bearer token 类型不符合控制台写接口要求: %w", err)
			}
			return nil, fmt.Errorf("set user policy denied: 当前 AK/SK 缺少 novaoperation.operationService.auth；这个写接口需要具备该 operation 权限的 AK/SK，或使用控制台 Bearer token（--bearer-token 或 RAYCTL_BEARER_TOKEN）: %w", err)
		}
		return nil, fmt.Errorf("set user policy: %w", err)
	}
	result.Result = "created"
	if len(resp.PolicyItems) > 0 {
		item := resp.PolicyItems[0]
		result.PolicyID = firstNonEmpty(item.PolicyID, item.ID)
		status := strings.TrimSpace(item.CreatePolicyStatus)
		if status != "" {
			switch strings.ToUpper(status) {
			case "ROLE_EXIST":
				result.Result = "already exists"
			case "SUCCESS", "CREATED", "CREATE_SUCCESS":
				result.Result = "created"
			default:
				result.Result = status
			}
		}
	}
	return result, nil
}

func (s *AuthService) RemoveAFS(ctx context.Context, req AuthGrantAFSRequest) (*AuthGrantAFSResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for auth remove")
	}
	resourceType := normalizeGrantResourceType(req.ResourceType)
	if resourceType == "" {
		resourceType = "afs"
	}
	resourceName := strings.TrimSpace(req.ResourceName)
	scope := strings.TrimSpace(req.Scope)
	if resourceName == "" && scope == "" {
		return nil, fmt.Errorf("%s name or scope is required", resourceType)
	}

	memberType := strings.ToUpper(strings.TrimSpace(req.MemberType))
	if memberType != "USER" && memberType != "GROUP" {
		return nil, fmt.Errorf("member type must be USER or GROUP")
	}
	memberName, memberIdentify, memberValue, err := s.resolveGrantMember(ctx, memberType, req.MemberIdentifier)
	if err != nil {
		return nil, err
	}

	if scope == "" {
		resource, err := s.findGrantResource(ctx, resourceType, resourceName)
		if err != nil {
			return nil, fmt.Errorf("resolve %s scope: %w", resourceType, err)
		}
		scope = ensureRMScope(resource.RID)
		if resourceName == "" {
			resourceName = strings.TrimSpace(resource.Name)
		}
	}
	if resourceName == "" {
		resourceName = extractResourceNameFromScope(scope)
	}
	if scope == "" {
		return nil, fmt.Errorf("%s scope is empty", resourceType)
	}

	roleName, roleID, err := s.resolveGrantRole(ctx, resourceType, req.Role)
	if err != nil {
		return nil, err
	}

	result := &AuthGrantAFSResult{
		ResourceType:   strings.ToUpper(resourceType),
		ResourceName:   resourceName,
		AFSName:        resourceName,
		Scope:          scope,
		MemberType:     memberType,
		MemberName:     memberName,
		MemberIdentify: memberIdentify,
		MemberValue:    memberValue,
		RoleName:       roleName,
		RoleID:         roleID,
	}

	relationPayload := platform.IAMMemberRelationPoliciesRequest{
		MemberType:  memberType,
		MemberValue: memberValue,
		MemberID:    memberValue,
		PageSize:    200,
		PageToken:   "1",
	}
	payloadBytes, _ := json.MarshalIndent(relationPayload, "", "  ")
	result.Payload = string(payloadBytes)

	resp, err := s.vcClient.MemberRelationIAMPolicies(ctx, relationPayload, req.BearerToken)
	if err != nil {
		return nil, fmt.Errorf("list member relation policies: %w", err)
	}
	policyID := findRemovePolicyID(resp, scope, memberType, memberValue, roleName, roleID)
	if policyID == "" {
		result.Result = "not found"
		return result, nil
	}
	result.PolicyID = policyID
	if req.DryRun {
		result.Result = "dry-run"
		return result, nil
	}
	if err := s.vcClient.DeleteIAMPolicy(ctx, policyID, memberValue, memberType, req.BearerToken); err != nil {
		return nil, fmt.Errorf("delete policy: %w", err)
	}
	result.Result = "removed"
	return result, nil
}

func extractAFSNameFromScope(scope string) string {
	return extractResourceNameFromScope(scope)
}

func extractResourceNameFromScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}
	scope = strings.Trim(scope, "/")
	if scope == "" {
		return ""
	}
	parts := strings.Split(scope, "/")
	name := parts[len(parts)-1]
	if cut := strings.IndexAny(name, "/?#"); cut >= 0 {
		name = name[:cut]
	}
	return strings.TrimSpace(name)
}

func (s *AuthService) resolveGrantMember(ctx context.Context, memberType string, identifier string) (string, string, string, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return "", "", "", fmt.Errorf("member identifier is required")
	}
	switch memberType {
	case "USER":
		users, err := s.vcClient.FindUsers(ctx, identifier)
		if err != nil {
			return "", "", "", err
		}
		if len(users) != 1 {
			return "", "", "", fmt.Errorf("user %q matched %d users, please use exact username or id", identifier, len(users))
		}
		user := users[0]
		return strings.TrimSpace(user.Name), strings.TrimSpace(user.Username), strings.TrimSpace(user.ID), nil
	case "GROUP":
		groups, err := s.vcClient.FindGroups(ctx, identifier)
		if err != nil {
			return "", "", "", err
		}
		if len(groups) != 1 {
			return "", "", "", fmt.Errorf("group %q matched %d groups, please use exact group name or id", identifier, len(groups))
		}
		group := groups[0]
		return firstNonEmpty(group.DisplayName, group.Name), firstNonEmpty(group.PosixGroupName, group.Name), strings.TrimSpace(group.ID), nil
	default:
		return "", "", "", fmt.Errorf("unsupported member type %q", memberType)
	}
}

func (s *AuthService) resolveAFSRole(ctx context.Context, role string) (string, string, error) {
	return s.resolveGrantRole(ctx, "afs", role)
}

func (s *AuthService) resolveGrantRole(ctx context.Context, resourceType string, role string) (string, string, error) {
	roleName := normalizeGrantRoleName(resourceType, role)
	if roleName == "" {
		return "", "", fmt.Errorf("role is required")
	}
	roles, err := s.vcClient.ListIAMRoles(ctx)
	if err != nil {
		return "", "", fmt.Errorf("list roles: %w", err)
	}
	for _, item := range roles {
		if strings.TrimSpace(item.RoleName) != roleName {
			continue
		}
		if strings.TrimSpace(item.ID) == "" {
			return "", "", fmt.Errorf("role %q has empty id", roleName)
		}
		return roleName, strings.TrimSpace(item.ID), nil
	}
	return "", "", fmt.Errorf("role %q not found", roleName)
}

func normalizeAFSRoleName(role string) string {
	return normalizeGrantRoleName("afs", role)
}

func grantRoleDefinitions(resourceType string) []authRoleDefinition {
	switch normalizeGrantResourceType(resourceType) {
	case "afs":
		return []authRoleDefinition{
			{Alias: "editor", RoleName: "afs.volumeEditor"},
			{Alias: "reader", RoleName: "afs.volumeReader"},
			{Alias: "owner", RoleName: "afs.volumeOwner"},
		}
	case "vc":
		return []authRoleDefinition{
			{Alias: "user", RoleName: "ecp.clusterUser"},
			{Alias: "admin", RoleName: "ecp.clusterAdmin"},
		}
	case "ccr":
		return []authRoleDefinition{
			{Alias: "user", RoleName: "ccr.namespaceUser"},
			{Alias: "imageUser", RoleName: "ccr.imageUser"},
			{Alias: "owner", RoleName: "ccr.namespaceOwner"},
		}
	case "subnet":
		return []authRoleDefinition{
			{Alias: "reader", RoleName: "vpc.reader"},
			{Alias: "editor", RoleName: "vpc.editor"},
		}
	case "ais":
		return []authRoleDefinition{
			{Alias: "owner", RoleName: "ais.instanceOwner"},
		}
	default:
		return nil
	}
}

func normalizeGrantRoleName(resourceType string, role string) string {
	resourceType = normalizeGrantResourceType(resourceType)
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "":
		switch resourceType {
		case "afs":
			return "afs.volumeEditor"
		case "vc":
			return "ecp.clusterUser"
		case "ccr":
			return "ccr.namespaceUser"
		case "subnet":
			return "vpc.reader"
		case "ais":
			return "ais.instanceOwner"
		default:
			return ""
		}
	}

	normalized := strings.ToLower(strings.TrimSpace(role))
	switch resourceType {
	case "afs":
		switch normalized {
		case "editor", "edit", "developer", "dev":
			return "afs.volumeEditor"
		case "reader", "read", "viewer", "view":
			return "afs.volumeReader"
		case "owner":
			return "afs.volumeOwner"
		}
	case "vc":
		switch normalized {
		case "user", "clusteruser", "cluster-user":
			return "ecp.clusterUser"
		case "admin", "clusteradmin", "cluster-admin":
			return "ecp.clusterAdmin"
		}
	case "ccr":
		switch normalized {
		case "user", "namespaceuser", "namespace-user":
			return "ccr.namespaceUser"
		case "imageuser", "image-user", "image":
			return "ccr.imageUser"
		case "owner", "namespaceowner", "namespace-owner":
			return "ccr.namespaceOwner"
		}
	case "subnet":
		switch normalized {
		case "reader", "read", "viewer", "view":
			return "vpc.reader"
		case "editor", "edit":
			return "vpc.editor"
		}
	case "ais":
		switch normalized {
		case "owner", "instanceowner", "instance-owner":
			return "ais.instanceOwner"
		}
	}
	return strings.TrimSpace(role)
}

func grantRoleAlias(resourceType string, roleName string) string {
	resourceType = normalizeGrantResourceType(resourceType)
	roleName = strings.TrimSpace(roleName)
	for _, def := range grantRoleDefinitions(resourceType) {
		if def.RoleName == roleName {
			return def.Alias
		}
	}
	return roleName
}

func shouldHideOwnRole(displayName string, roleName string) bool {
	hidden := strings.TrimSpace(displayName)
	if hidden == "" {
		hidden = strings.TrimSpace(roleName)
	}
	switch hidden {
	case "所有 get 权限", "所有 list 权限":
		return true
	default:
		return false
	}
}

func normalizeGrantResourceType(resourceType string) string {
	switch strings.ToLower(strings.TrimSpace(resourceType)) {
	case "", "afs", "aoss", "volume", "virtualvolume", "virtual-volume":
		return "afs"
	case "vc", "cluster", "virtualcluster", "virtual-cluster":
		return "vc"
	case "ccr", "namespace", "ns":
		return "ccr"
	case "subnet":
		return "subnet"
	case "ais", "ai", "aispace", "ai-space", "ai_spaces":
		return "ais"
	default:
		return strings.ToLower(strings.TrimSpace(resourceType))
	}
}

func (s *AuthService) findGrantResource(ctx context.Context, resourceType string, name string) (*platform.StorageVolumeResource, error) {
	switch normalizeGrantResourceType(resourceType) {
	case "afs":
		return s.vcClient.FindStorageVolumeResource(ctx, name)
	case "vc":
		return s.vcClient.FindResourceByName(ctx, name, "virtualClusters")
	case "ccr":
		return s.vcClient.FindResourceByName(ctx, name, "namespaces")
	case "subnet":
		return s.vcClient.FindResourceByName(ctx, name, "subnets")
	case "ais":
		return s.vcClient.FindResourceByName(ctx, name, "aiSpaces")
	default:
		return nil, fmt.Errorf("unsupported resource type %q", resourceType)
	}
}

func ensureRMScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}
	if strings.HasPrefix(scope, "/rm/") {
		return scope
	}
	if strings.HasPrefix(scope, "/subscriptions/") {
		return "/rm" + scope
	}
	return scope
}

func emptyIAMPolicyCondition() platform.IAMPolicyCondition {
	return platform.IAMPolicyCondition{
		DateNotEquals:         []string{},
		DateLessThan:          []string{},
		DateLessThanEquals:    []string{},
		DateGreaterThan:       []string{},
		DateGreaterThanEquals: []string{},
		StringEquals:          []string{},
	}
}

func existingGrantPolicyID(policies []platform.IAMBindingPolicy, scope string, memberType string, memberValue string, roleName string) string {
	for _, policy := range policies {
		if !strings.EqualFold(strings.TrimSpace(policy.MemberType), strings.TrimSpace(memberType)) {
			continue
		}
		if strings.TrimSpace(policy.MemberValue) != strings.TrimSpace(memberValue) {
			continue
		}
		if normalizeAuthScope(policy.Scope) != normalizeAuthScope(scope) && ensureRMScope(policy.Scope) != ensureRMScope(scope) {
			continue
		}
		for _, role := range policy.RoleInfos {
			if strings.TrimSpace(role.RoleName) == roleName {
				return strings.TrimSpace(policy.ID)
			}
		}
	}
	return ""
}

func findRemovePolicyID(resp *platform.IAMMemberRelationPoliciesResponse, scope string, memberType string, memberValue string, roleName string, roleID string) string {
	if resp == nil {
		return ""
	}
	for _, policy := range resp.Policies {
		if !policyMatchesRemoveTarget(policy.ID, policy.Scope, policy.MemberType, policy.MemberValue, policy.RoleInfos, "", scope, memberType, memberValue, roleName, roleID) {
			continue
		}
		return strings.TrimSpace(policy.ID)
	}
	for _, item := range append(resp.PolicyItems, resp.Items...) {
		policyID := firstNonEmpty(item.PolicyID, item.ID)
		if !policyMatchesRemoveTarget(policyID, item.Scope, item.MemberType, item.MemberValue, item.RoleInfos, item.RoleID, scope, memberType, memberValue, roleName, roleID) {
			continue
		}
		return strings.TrimSpace(policyID)
	}
	return ""
}

func policyMatchesRemoveTarget(policyID string, policyScope string, policyMemberType string, policyMemberValue string, roles []platform.IAMRoleInfo, policyRoleID string, scope string, memberType string, memberValue string, roleName string, roleID string) bool {
	if strings.TrimSpace(policyID) == "" {
		return false
	}
	if normalizeAuthScope(policyScope) != normalizeAuthScope(scope) && ensureRMScope(policyScope) != ensureRMScope(scope) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(policyMemberType), strings.TrimSpace(memberType)) {
		return false
	}
	if strings.TrimSpace(policyMemberValue) != strings.TrimSpace(memberValue) {
		return false
	}
	if strings.TrimSpace(policyRoleID) != "" && strings.TrimSpace(policyRoleID) == strings.TrimSpace(roleID) {
		return true
	}
	for _, role := range roles {
		if strings.TrimSpace(role.ID) == strings.TrimSpace(roleID) {
			return true
		}
		if strings.TrimSpace(role.RoleName) == strings.TrimSpace(roleName) {
			return true
		}
	}
	return false
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
		scopeI := authScopeSortKey(items[i].Scope)
		scopeJ := authScopeSortKey(items[j].Scope)
		if scopeI != scopeJ {
			return scopeI < scopeJ
		}
		if items[i].Roles != items[j].Roles {
			return items[i].Roles < items[j].Roles
		}
		if items[i].RoleNames != items[j].RoleNames {
			return items[i].RoleNames < items[j].RoleNames
		}
		if items[i].Service != items[j].Service {
			return items[i].Service < items[j].Service
		}
		if items[i].Source != items[j].Source {
			return items[i].Source < items[j].Source
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

func authScopeSortKey(scope string) string {
	scope = strings.TrimSpace(scope)
	scope = strings.TrimPrefix(scope, "/rm")
	scope = strings.Trim(scope, "/")
	if scope == "" {
		return "租户级"
	}
	parts := strings.Split(scope, "/")
	if len(parts) == 1 {
		switch strings.ToLower(parts[0]) {
		case "tenant", "tenants":
			return "租户级"
		}
	}
	if len(parts) >= 2 {
		switch parts[0] {
		case "managementGroups", "managementgroups":
			if len(parts) == 2 {
				return "管理组"
			}
			return parts[len(parts)-1]
		case "subscriptions":
			if len(parts) == 2 {
				return "订阅级"
			}
			if len(parts) == 4 && parts[2] == "resourceGroups" {
				return "资源组 " + parts[3]
			}
			if len(parts) == 6 && parts[2] == "resourceGroups" && parts[4] == "zones" {
				return "可用区 " + parts[5]
			}
			if len(parts) == 6 && parts[2] == "resourceGroups" && parts[4] == "regions" {
				return "地域 " + parts[5]
			}
			if len(parts) > 4 {
				return parts[len(parts)-1]
			}
		case "tenants", "tenant":
			return "租户级"
		}
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
