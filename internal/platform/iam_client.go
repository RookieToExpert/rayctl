package platform

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"rayctl/internal/concurrent"
)

const iamSubjectPolicyBatchSize = 5

type IAMUser struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Username      string `json:"username"`
	TenantCode    string `json:"tenant_code"`
	Status        string `json:"status"`
	Source        string `json:"source"`
	ConsoleState  string `json:"console_state"`
	OpenAPIState  string `json:"open_api_state"`
	MFAState      string `json:"mfa_state"`
	CreateTime    string `json:"create_time"`
	LastLoginTime string `json:"last_login_time"`
	JoinTime      string `json:"join_time"`
}

type IAMGroup struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	PosixGroupName string `json:"posix_group_name"`
	TenantID       string `json:"tenant_id"`
	Status         string `json:"status"`
	CreateTime     string `json:"create_time"`
}

type IAMUserGroupSearchItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
}

type IAMBindingPolicy struct {
	ID             string        `json:"id"`
	Scope          string        `json:"scope"`
	MemberType     string        `json:"member_type"`
	MemberName     string        `json:"member_name"`
	MemberIdentify string        `json:"member_identify"`
	MemberValue    string        `json:"member_value"`
	Level          string        `json:"level"`
	Service        string        `json:"service"`
	CreateTime     string        `json:"create_time"`
	UpdateTime     string        `json:"update_time"`
	RoleInfos      []IAMRoleInfo `json:"role_infos"`
}

type IAMRoleInfo struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	Description      string `json:"description"`
	RoleName         string `json:"role_name"`
	AvailableService string `json:"available_service"`
}

type IAMSetPolicyRequest struct {
	Scope          string                `json:"scope,omitempty"`
	MemberType     string                `json:"member_type,omitempty"`
	MemberValue    string                `json:"member_value,omitempty"`
	RoleIDs        []string              `json:"role_ids,omitempty"`
	ExcludeMembers []string              `json:"exclude_members"`
	Condition      IAMPolicyCondition    `json:"condition"`
	MemberValues   []string              `json:"member_values,omitempty"`
	Policies       []IAMSetPolicyPayload `json:"policies,omitempty"`
}

type IAMSetPolicyPayload struct {
	Scope          string             `json:"scope"`
	RoleIDs        []string           `json:"role_ids"`
	ExcludeMembers []string           `json:"exclude_members"`
	Condition      IAMPolicyCondition `json:"condition"`
}

type IAMPolicyCondition struct {
	DateNotEquals         []string `json:"date_not_equals"`
	DateLessThan          []string `json:"date_less_than"`
	DateLessThanEquals    []string `json:"date_less_than_equals"`
	DateGreaterThan       []string `json:"date_greater_than"`
	DateGreaterThanEquals []string `json:"date_greater_than_equals"`
	StringEquals          []string `json:"string_equals"`
}

type IAMSetPolicyResponse struct {
	ID       string             `json:"id"`
	PolicyID string             `json:"policy_id"`
	Policy   IAMBindingPolicy   `json:"policy"`
	Policies []IAMBindingPolicy `json:"policies"`
}

type IAMBatchCreatePoliciesRequest struct {
	Policies []IAMBatchCreatePolicy `json:"policies"`
}

type IAMBatchCreatePolicy struct {
	Scope          string                 `json:"scope"`
	RoleIDs        []string               `json:"role_ids"`
	Members        []IAMBatchCreateMember `json:"members"`
	ExcludeMembers []IAMBatchCreateMember `json:"exclude_members"`
	Level          string                 `json:"level"`
	Condition      *IAMPolicyCondition    `json:"condition,omitempty"`
}

type IAMBatchCreateMember struct {
	MemberType  string `json:"member_type"`
	MemberValue string `json:"member_value"`
}

type IAMBatchCreatePoliciesResponse struct {
	PolicyItems []IAMBatchCreatePolicyItem `json:"policy_item"`
}

type IAMBatchCreatePolicyItem struct {
	Member             IAMBatchCreateMember `json:"member"`
	CreatePolicyStatus string               `json:"create_policy_status"`
	RoleID             string               `json:"role_id"`
	Scope              string               `json:"scope"`
	PolicyID           string               `json:"policy_id"`
	ID                 string               `json:"id"`
}

type IAMMemberRelationPoliciesRequest struct {
	MemberType  string `json:"member_type,omitempty"`
	MemberValue string `json:"member_value,omitempty"`
	MemberID    string `json:"member_id,omitempty"`
	PageSize    int    `json:"page_size,omitempty"`
	PageToken   string `json:"page_token,omitempty"`
}

type IAMMemberRelationPoliciesResponse struct {
	Policies      []IAMBindingPolicy        `json:"policies"`
	PolicyItems   []IAMMemberRelationPolicy `json:"policy_item"`
	Items         []IAMMemberRelationPolicy `json:"items"`
	NextPageToken string                    `json:"next_page_token"`
}

type IAMMemberRelationPolicy struct {
	ID             string        `json:"id"`
	PolicyID       string        `json:"policy_id"`
	Scope          string        `json:"scope"`
	MemberType     string        `json:"member_type"`
	MemberValue    string        `json:"member_value"`
	MemberName     string        `json:"member_name"`
	MemberIdentify string        `json:"member_identify"`
	RoleID         string        `json:"role_id"`
	RoleInfos      []IAMRoleInfo `json:"role_infos"`
}

type iamUserListResponse struct {
	Users         []IAMUser `json:"users"`
	NextPageToken string    `json:"next_page_token"`
	TotalSize     int       `json:"total_size"`
}

type iamGroupListResponse struct {
	Groups        []IAMGroup `json:"groups"`
	NextPageToken string     `json:"next_page_token"`
	TotalSize     int        `json:"total_size"`
}

type iamUserGroupSearchResponse struct {
	Items      []IAMUserGroupSearchItem `json:"items"`
	Users      []IAMUserGroupSearchItem `json:"users"`
	Groups     []IAMUserGroupSearchItem `json:"groups"`
	UserGroups []IAMUserGroupSearchItem `json:"user_groups"`
}

type iamBindingPolicyListResponse struct {
	Policies      []IAMBindingPolicy `json:"policies"`
	NextPageToken string             `json:"next_page_token"`
	TotalSize     int                `json:"total_size"`
}

type iamRoleListResponse struct {
	Roles         []IAMRoleInfo `json:"roles"`
	RoleInfos     []IAMRoleInfo `json:"role_infos"`
	Items         []IAMRoleInfo `json:"items"`
	NextPageToken string        `json:"next_page_token"`
	TotalSize     int           `json:"total_size"`
}

type iamResourceScope struct {
	ID                  string `json:"id"`
	Scope               string `json:"scope"`
	ResourceScope       string `json:"resource_scope"`
	RID                 string `json:"rid"`
	Name                string `json:"name"`
	ResourceName        string `json:"resource_name"`
	DisplayName         string `json:"display_name"`
	ResourceDisplayName string `json:"resource_display_name"`
	ResourceType        string `json:"resource_type"`
	Type                string `json:"type"`
}

type iamResourceScopeListResponse struct {
	Scopes         []iamResourceScope `json:"scopes"`
	ResourceScopes []iamResourceScope `json:"resource_scopes"`
	ScopeInfos     []iamResourceScope `json:"scope_infos"`
	Resources      []iamResourceScope `json:"resources"`
	Items          []iamResourceScope `json:"items"`
	NextPageToken  string             `json:"next_page_token"`
}

func (c *VirtualClusterClient) resolveUsernamesWithProfile(ctx context.Context, profile clientProfile, ids []string) (map[string]string, error) {
	result := make(map[string]string, len(ids))
	for start := 0; start < len(ids); start += defaultPageLimit {
		end := start + defaultPageLimit
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		filters := make([]string, 0, len(chunk))
		for _, id := range chunk {
			filters = append(filters, fmt.Sprintf(`id="%s"`, id))
		}

		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/idp/v1/getUsers"
		query := u.Query()
		query.Set("includeAdmin", "true")
		query.Set("page_token", "1")
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("order_by", "create_time desc")
		query.Set("filter", strings.Join(filters, " OR "))
		u.RawQuery = query.Encode()

		var payload iamUserListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		for _, user := range payload.Users {
			result[user.ID] = firstNonEmpty(user.Username, user.Name, user.ID)
		}
	}
	return result, nil
}

func (c *VirtualClusterClient) resolveUserIDsWithProfile(ctx context.Context, profile clientProfile, usernames []string) (map[string]string, error) {
	result := make(map[string]string, len(usernames))
	for start := 0; start < len(usernames); start += defaultPageLimit {
		end := start + defaultPageLimit
		if end > len(usernames) {
			end = len(usernames)
		}
		chunk := usernames[start:end]
		filters := make([]string, 0, len(chunk))
		for _, username := range chunk {
			filters = append(filters, fmt.Sprintf(`username="%s"`, escapeIAMFilterValue(username)))
		}

		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/idp/v1/getUsers"
		query := u.Query()
		query.Set("includeAdmin", "true")
		query.Set("page_token", "1")
		query.Set("page_size", fmt.Sprintf("%d", defaultPageLimit))
		query.Set("order_by", "create_time desc")
		query.Set("filter", strings.Join(filters, " OR "))
		u.RawQuery = query.Encode()

		var payload iamUserListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return nil, err
		}
		for _, user := range payload.Users {
			username := strings.TrimSpace(user.Username)
			id := strings.TrimSpace(user.ID)
			if username != "" && id != "" {
				result[strings.ToLower(username)] = id
			}
		}
	}
	return result, nil
}

func escapeIAMFilterValue(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func (c *VirtualClusterClient) listUsersForProfile(ctx context.Context, profile clientProfile) ([]IAMUser, error) {
	return c.listUsersForProfileLimit(ctx, profile, 0, true)
}

func (c *VirtualClusterClient) ListIAMUsersForProfile(ctx context.Context, profileName string, limit int) ([]IAMUser, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listUsersForProfileLimit(ctx, profile, limit, false)
}

func (c *VirtualClusterClient) listUsersForProfileLimit(ctx context.Context, profile clientProfile, limit int, validOnly bool) ([]IAMUser, error) {
	return collectIAMPages(ctx, defaultPageLimit, limit, false, func(token string, size int) (iamPage[IAMUser], error) {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/idp/v1/users"
		q := u.Query()
		q.Set("page_size", strconv.Itoa(size))
		q.Set("page_token", token)
		if validOnly {
			q.Set("filter", `(status="valid")`)
		}
		u.RawQuery = q.Encode()
		var payload iamUserListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return iamPage[IAMUser]{}, err
		}
		return iamPage[IAMUser]{payload.Users, payload.NextPageToken, payload.TotalSize}, nil
	})
}

func (c *VirtualClusterClient) findExactUsersForProfile(ctx context.Context, profile clientProfile, identifier string) ([]IAMUser, error) {
	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/idp/v1/getUsers"
	query := u.Query()
	query.Set("includeAdmin", "true")
	query.Set("page_token", "1")
	query.Set("page_size", "10")
	query.Set("order_by", "create_time desc")
	escaped := escapeIAMFilterValue(identifier)
	query.Set("filter", fmt.Sprintf(`username="%s" OR id="%s"`, escaped, escaped))
	u.RawQuery = query.Encode()

	var payload iamUserListResponse
	if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
		return nil, err
	}

	result := make([]IAMUser, 0, len(payload.Users))
	for _, user := range payload.Users {
		if strings.EqualFold(strings.TrimSpace(user.Username), identifier) || strings.EqualFold(strings.TrimSpace(user.ID), identifier) {
			result = append(result, user)
		}
	}
	return result, nil
}

func (c *VirtualClusterClient) findDetailedExactUsersForProfile(ctx context.Context, profile clientProfile, identifier string, exact []IAMUser) ([]IAMUser, error) {
	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/idp/v1/users"
	query := u.Query()
	query.Set("page_token", "1")
	query.Set("page_size", "10")
	escaped := escapeIAMFilterValue(identifier)
	field := "username"
	for _, user := range exact {
		if strings.EqualFold(strings.TrimSpace(user.ID), identifier) {
			field = "id"
			break
		}
	}
	query.Set("filter", fmt.Sprintf(`(status="valid") AND (%s="%s")`, field, escaped))
	u.RawQuery = query.Encode()

	var payload iamUserListResponse
	if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
		return nil, err
	}
	return payload.Users, nil
}

func enrichExactIAMUsers(exact []IAMUser, detailed []IAMUser) []IAMUser {
	byID := make(map[string]IAMUser, len(detailed))
	byUsername := make(map[string]IAMUser, len(detailed))
	for _, user := range detailed {
		if id := strings.ToLower(strings.TrimSpace(user.ID)); id != "" {
			byID[id] = user
		}
		if username := strings.ToLower(strings.TrimSpace(user.Username)); username != "" {
			byUsername[username] = user
		}
	}
	for index := range exact {
		user, ok := byID[strings.ToLower(strings.TrimSpace(exact[index].ID))]
		if !ok {
			user, ok = byUsername[strings.ToLower(strings.TrimSpace(exact[index].Username))]
		}
		if !ok {
			continue
		}
		if strings.TrimSpace(exact[index].TenantCode) == "" {
			exact[index].TenantCode = user.TenantCode
		}
		if strings.TrimSpace(exact[index].Source) == "" {
			exact[index].Source = user.Source
		}
	}
	return exact
}

func (c *VirtualClusterClient) FindUsers(ctx context.Context, identifier string) ([]IAMUser, error) {
	return c.FindUsersForProfile(ctx, "", identifier)
}

func (c *VirtualClusterClient) FindUsersForProfile(ctx context.Context, profileName string, identifier string) ([]IAMUser, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}

	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("user identifier is required")
	}

	// Most callers provide a complete username or user ID. Resolve that with one
	// filtered request before falling back to the paginated fuzzy search.
	exact, exactErr := c.findExactUsersForProfile(ctx, profile, identifier)
	if len(exact) > 0 {
		needsDetails := false
		for _, user := range exact {
			if strings.TrimSpace(user.TenantCode) == "" || strings.TrimSpace(user.Source) == "" {
				needsDetails = true
				break
			}
		}
		if needsDetails {
			if detailed, detailErr := c.findDetailedExactUsersForProfile(ctx, profile, identifier, exact); detailErr == nil {
				exact = enrichExactIAMUsers(exact, detailed)
			}
		}
		sort.Slice(exact, func(i, j int) bool {
			return strings.TrimSpace(exact[i].Username) < strings.TrimSpace(exact[j].Username)
		})
		return exact, nil
	}

	if err := canceledIAMUserLookup(ctx, exactErr); err != nil {
		return nil, err
	}
	result, err := c.findUsersForProfileFallback(ctx, profile, identifier)
	if err != nil {
		if exactErr != nil {
			return nil, fmt.Errorf("find exact user: %v; list users: %w", exactErr, err)
		}
		return nil, fmt.Errorf("list users: %w", err)
	}

	if len(result) == 0 {
		return nil, &IAMUserNotFoundError{Identifier: identifier}
	}
	return result, nil
}

func (c *VirtualClusterClient) ListUserGroups(ctx context.Context, userID string) ([]IAMGroup, error) {
	return c.ListUserGroupsForProfile(ctx, "", userID)
}

func (c *VirtualClusterClient) ListUserGroupsForProfile(ctx context.Context, profileName string, userID string) ([]IAMGroup, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	return collectIAMPages(ctx, defaultPageLimit, 0, false, func(token string, size int) (iamPage[IAMGroup], error) {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = fmt.Sprintf("/iam/idp/v1/users/%s:getGroups", url.PathEscape(userID))
		q := u.Query()
		q.Set("page_size", strconv.Itoa(size))
		q.Set("page_token", token)
		q.Set("order_by", "create_time desc")
		u.RawQuery = q.Encode()
		var payload iamGroupListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return iamPage[IAMGroup]{}, err
		}
		return iamPage[IAMGroup]{payload.Groups, payload.NextPageToken, payload.TotalSize}, nil
	})
}

func (c *VirtualClusterClient) ListGroupUsersForProfile(ctx context.Context, profileName string, groupID string) ([]IAMUser, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, fmt.Errorf("group id is required")
	}
	return collectIAMPages(ctx, defaultPageLimit, 0, false, func(token string, size int) (iamPage[IAMUser], error) {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = fmt.Sprintf("/iam/idp/v1/groups/%s:getUsers", url.PathEscape(groupID))
		q := u.Query()
		q.Set("page_size", strconv.Itoa(size))
		q.Set("page_token", token)
		q.Set("order_by", "join_time desc")
		u.RawQuery = q.Encode()
		var payload iamUserListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return iamPage[IAMUser]{}, err
		}
		return iamPage[IAMUser]{payload.Users, payload.NextPageToken, payload.TotalSize}, nil
	})
}

func (c *VirtualClusterClient) listGroupsForProfile(ctx context.Context, profile clientProfile) ([]IAMGroup, error) {
	return c.listGroupsForProfileLimit(ctx, profile, 0)
}

func (c *VirtualClusterClient) ListIAMGroupsForProfile(ctx context.Context, profileName string, limit int) ([]IAMGroup, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listGroupsForProfileLimit(ctx, profile, limit)
}

func (c *VirtualClusterClient) listGroupsForProfileLimit(ctx context.Context, profile clientProfile, limit int) ([]IAMGroup, error) {
	return collectIAMPages(ctx, defaultPageLimit, limit, false, func(token string, size int) (iamPage[IAMGroup], error) {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/idp/v1/groups"
		q := u.Query()
		q.Set("page_size", strconv.Itoa(size))
		q.Set("page_token", token)
		q.Set("order_by", "create_time desc")
		u.RawQuery = q.Encode()
		var payload iamGroupListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return iamPage[IAMGroup]{}, err
		}
		return iamPage[IAMGroup]{payload.Groups, payload.NextPageToken, payload.TotalSize}, nil
	})
}

func (c *VirtualClusterClient) FindGroups(ctx context.Context, identifier string) ([]IAMGroup, error) {
	return c.FindGroupsForProfile(ctx, "", identifier)
}

func (c *VirtualClusterClient) FindGroupsForProfile(ctx context.Context, profileName string, identifier string) ([]IAMGroup, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}

	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("group identifier is required")
	}

	groups, err := c.listGroupsForProfile(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}

	exact := make([]IAMGroup, 0)
	prefix := make([]IAMGroup, 0)
	contains := make([]IAMGroup, 0)
	normalized := strings.ToLower(identifier)
	for _, group := range groups {
		fields := []string{
			strings.TrimSpace(group.ID),
			strings.TrimSpace(group.Name),
			strings.TrimSpace(group.DisplayName),
			strings.TrimSpace(group.PosixGroupName),
		}
		matchedExact := false
		matchedPrefix := false
		matchedContains := false
		for _, field := range fields {
			if field == "" {
				continue
			}
			lower := strings.ToLower(field)
			switch {
			case lower == normalized:
				matchedExact = true
			case strings.HasPrefix(lower, normalized):
				matchedPrefix = true
			case strings.Contains(lower, normalized):
				matchedContains = true
			}
		}
		switch {
		case matchedExact:
			exact = append(exact, group)
		case matchedPrefix:
			prefix = append(prefix, group)
		case matchedContains:
			contains = append(contains, group)
		}
	}

	result := exact
	if len(result) == 0 {
		result = prefix
	}
	if len(result) == 0 {
		result = contains
	}
	sort.Slice(result, func(i, j int) bool {
		left := firstNonEmpty(result[i].DisplayName, result[i].Name, result[i].PosixGroupName, result[i].ID)
		right := firstNonEmpty(result[j].DisplayName, result[j].Name, result[j].PosixGroupName, result[j].ID)
		if left != right {
			return left < right
		}
		return strings.TrimSpace(result[i].ID) < strings.TrimSpace(result[j].ID)
	})
	if len(result) == 0 {
		return nil, fmt.Errorf("group %q not found in current tenant", identifier)
	}
	return result, nil
}

func (c *VirtualClusterClient) ListIAMBindingPolicies(ctx context.Context) ([]IAMBindingPolicy, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	return c.listIAMBindingPoliciesWithProfile(ctx, profile, "")
}

func (c *VirtualClusterClient) ListIAMBindingPoliciesForProfile(ctx context.Context, profileName string) ([]IAMBindingPolicy, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.listIAMBindingPoliciesWithProfile(ctx, profile, "")
}

func (c *VirtualClusterClient) ListIAMBindingPoliciesForResourceProfile(ctx context.Context, profileName string, resourceName string) ([]IAMBindingPolicy, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	resourceName = escapeIAMFilterValue(resourceName)
	filter := ""
	if resourceName != "" {
		filter = fmt.Sprintf(
			`(member_identify="*%s*" OR role_name="*%s*" OR scope="*%s*")`,
			resourceName,
			resourceName,
			resourceName,
		)
	}
	return c.listIAMBindingPoliciesWithProfile(ctx, profile, filter)
}

func (c *VirtualClusterClient) ListIAMBindingPoliciesForSubjectsProfile(ctx context.Context, profileName string, identifiers []string) ([]IAMBindingPolicy, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	unique := make([]string, 0, len(identifiers))
	seenIdentifiers := make(map[string]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		identifier = strings.TrimSpace(identifier)
		key := strings.ToLower(identifier)
		if key == "" {
			continue
		}
		if _, exists := seenIdentifiers[key]; exists {
			continue
		}
		seenIdentifiers[key] = struct{}{}
		unique = append(unique, identifier)
	}
	if len(unique) == 0 {
		return nil, nil
	}

	filters := make([]string, 0, (len(unique)+iamSubjectPolicyBatchSize-1)/iamSubjectPolicyBatchSize)
	for start := 0; start < len(unique); start += iamSubjectPolicyBatchSize {
		end := min(start+iamSubjectPolicyBatchSize, len(unique))
		terms := make([]string, 0, (end-start)*2)
		for _, identifier := range unique[start:end] {
			escaped := escapeIAMFilterValue(identifier)
			terms = append(terms,
				fmt.Sprintf(`member_identify="*%s*"`, escaped),
				fmt.Sprintf(`scope="*%s*"`, escaped),
			)
		}
		filters = append(filters, "("+strings.Join(terms, " OR ")+")")
	}
	type policyResult struct {
		items []IAMBindingPolicy
		err   error
	}
	chunks := concurrent.Map(ctx, filters, 4, func(ctx context.Context, filter string) policyResult {
		if err := ctx.Err(); err != nil {
			return policyResult{err: err}
		}
		items, err := c.listIAMBindingPoliciesWithProfile(ctx, profile, filter)
		return policyResult{items: items, err: err}
	})
	result := make([]IAMBindingPolicy, 0)
	type bindingIdentity struct {
		id             string
		scope          string
		memberType     string
		memberValue    string
		memberIdentify string
	}
	seenPolicies := make(map[bindingIdentity]struct{})
	for _, chunk := range chunks {
		if chunk.err != nil {
			return nil, chunk.err
		}
		for _, policy := range chunk.items {
			// A policy ID can grant the same roles to multiple members. Deduplicate
			// repeated bindings, rather than dropping other members of that policy.
			key := bindingIdentity{
				id:          strings.TrimSpace(policy.ID),
				scope:       strings.TrimSpace(policy.Scope),
				memberType:  strings.ToUpper(strings.TrimSpace(policy.MemberType)),
				memberValue: strings.TrimSpace(policy.MemberValue),
			}
			if key.id == "" || key.memberValue == "" {
				key.memberIdentify = strings.TrimSpace(policy.MemberIdentify)
			}
			if _, exists := seenPolicies[key]; exists {
				continue
			}
			seenPolicies[key] = struct{}{}
			result = append(result, policy)
		}
	}
	return result, nil
}

func (c *VirtualClusterClient) listIAMBindingPoliciesWithProfile(ctx context.Context, profile clientProfile, filter string) ([]IAMBindingPolicy, error) {
	return collectIAMPagesWithRetrySize(ctx, defaultPageLimit, iamBindingPolicyRetryPageSize, 0, true, func(token string, size int) (iamPage[IAMBindingPolicy], error) {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/authz/v1/bindingPolicies"
		q := u.Query()
		q.Set("pageSize", strconv.Itoa(size))
		q.Set("page_token", token)
		q.Set("order_by", "create_time desc")
		if strings.TrimSpace(filter) != "" {
			q.Set("filter", filter)
		}
		u.RawQuery = q.Encode()
		var payload iamBindingPolicyListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return iamPage[IAMBindingPolicy]{}, err
		}
		return iamPage[IAMBindingPolicy]{payload.Policies, payload.NextPageToken, payload.TotalSize}, nil
	})
}

func (c *VirtualClusterClient) ListIAMRoles(ctx context.Context) ([]IAMRoleInfo, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	return collectIAMPages(ctx, 200, 0, true, func(token string, size int) (iamPage[IAMRoleInfo], error) {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/authz/v1/roles"
		q := u.Query()
		q.Set("page_size", strconv.Itoa(size))
		q.Set("page_token", token)
		q.Set("filter", "")
		u.RawQuery = q.Encode()
		var payload iamRoleListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			return iamPage[IAMRoleInfo]{}, err
		}
		roles := payload.Roles
		if len(roles) == 0 {
			roles = payload.RoleInfos
		}
		if len(roles) == 0 {
			roles = payload.Items
		}
		return iamPage[IAMRoleInfo]{roles, payload.NextPageToken, payload.TotalSize}, nil
	})
}

func (c *VirtualClusterClient) ListOwnIAMRoles(ctx context.Context, scope string, bearerToken string) ([]IAMRoleInfo, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil, fmt.Errorf("scope is required")
	}
	return collectIAMPages(ctx, 100, 0, true, func(token string, size int) (iamPage[IAMRoleInfo], error) {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/authz/v1/roles:own"
		q := u.Query()
		q.Set("page_size", strconv.Itoa(size))
		q.Set("page_token", token)
		q.Set("scope", scope)
		q.Set("level", "resources")
		u.RawQuery = q.Encode()
		var payload iamRoleListResponse
		if err := c.getJSONWithBearerProfile(ctx, profile, u.String(), bearerToken, &payload); err != nil {
			return iamPage[IAMRoleInfo]{}, err
		}
		roles := payload.Roles
		if len(roles) == 0 {
			roles = payload.RoleInfos
		}
		if len(roles) == 0 {
			roles = payload.Items
		}
		return iamPage[IAMRoleInfo]{roles, payload.NextPageToken, payload.TotalSize}, nil
	})
}

func (c *VirtualClusterClient) FindIAMResourceScope(ctx context.Context, name string, searchTerm string, bearerToken string) (*StorageVolumeResource, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	name = strings.TrimSpace(name)
	searchTerm = strings.TrimSpace(searchTerm)
	bearerToken = strings.TrimSpace(bearerToken)
	if name == "" {
		return nil, fmt.Errorf("resource name is required")
	}
	if searchTerm == "" {
		searchTerm = name
	}
	if bearerToken == "" {
		return nil, fmt.Errorf("bearer token is required for IAM resource scope lookup")
	}

	pageToken := "1"
	cursor := newPageCursor(pageToken)
	for {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/authz/v1/services/rm/levels/resources/scopes"
		query := u.Query()
		query.Set("filter", fmt.Sprintf(`name="*%s*" OR display_name="*%s*"`, searchTerm, searchTerm))
		query.Set("page_token", pageToken)
		query.Set("page_size", "50")
		query.Set("order_by", "create_time desc")
		u.RawQuery = query.Encode()

		var payload iamResourceScopeListResponse
		if err := c.getJSONWithBearerProfile(ctx, profile, u.String(), bearerToken, &payload); err != nil {
			return nil, err
		}
		items := payload.Scopes
		if len(items) == 0 {
			items = payload.ResourceScopes
		}
		if len(items) == 0 {
			items = payload.ScopeInfos
		}
		if len(items) == 0 {
			items = payload.Resources
		}
		if len(items) == 0 {
			items = payload.Items
		}
		for _, item := range items {
			itemName := firstNonEmpty(strings.TrimSpace(item.Name), strings.TrimSpace(item.ResourceName))
			displayName := firstNonEmpty(strings.TrimSpace(item.DisplayName), strings.TrimSpace(item.ResourceDisplayName))
			if !strings.EqualFold(itemName, name) && !strings.EqualFold(displayName, name) {
				continue
			}
			scope := firstNonEmpty(strings.TrimSpace(item.Scope), strings.TrimSpace(item.ResourceScope), strings.TrimSpace(item.RID))
			if scope == "" {
				continue
			}
			return &StorageVolumeResource{
				ID:           strings.TrimSpace(item.ID),
				RID:          scope,
				Name:         firstNonEmpty(itemName, name),
				DisplayName:  displayName,
				Type:         strings.TrimSpace(item.Type),
				ResourceType: strings.TrimSpace(item.ResourceType),
				ProfileName:  profile.Name,
			}, nil
		}

		nextPageToken := strings.TrimSpace(payload.NextPageToken)
		if len(items) == 0 || !cursor.advance(nextPageToken) {
			break
		}
		pageToken = nextPageToken
	}
	return nil, fmt.Errorf("IAM resource scope %q not found", name)
}

func (c *VirtualClusterClient) SetIAMPolicy(ctx context.Context, payload IAMSetPolicyRequest, bearerToken string) (*IAMSetPolicyResponse, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/authz/v1/Policies:setUserPolicy"

	var out IAMSetPolicyResponse
	var err error
	if strings.TrimSpace(bearerToken) != "" {
		err = c.postJSONWithBearerProfile(ctx, profile, u.String(), payload, strings.TrimSpace(bearerToken), &out)
	} else {
		err = c.postJSONWithProfile(ctx, profile, u.String(), payload, &out)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) BatchCreateIAMPolicies(ctx context.Context, payload IAMBatchCreatePoliciesRequest, bearerToken string) (*IAMBatchCreatePoliciesResponse, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/authz/v1/policies:batchCreate"

	var out IAMBatchCreatePoliciesResponse
	var err error
	if strings.TrimSpace(bearerToken) != "" {
		err = c.postJSONWithBearerProfile(ctx, profile, u.String(), payload, strings.TrimSpace(bearerToken), &out)
	} else {
		err = c.postJSONWithProfile(ctx, profile, u.String(), payload, &out)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) MemberRelationIAMPolicies(ctx context.Context, payload IAMMemberRelationPoliciesRequest, bearerToken string) (*IAMMemberRelationPoliciesResponse, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/authz/v1/policies:memberRelationPolicies"

	var out IAMMemberRelationPoliciesResponse
	var err error
	if strings.TrimSpace(bearerToken) != "" {
		err = c.postJSONWithBearerProfile(ctx, profile, u.String(), payload, strings.TrimSpace(bearerToken), &out)
	} else {
		err = c.postJSONWithProfile(ctx, profile, u.String(), payload, &out)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *VirtualClusterClient) DeleteIAMPolicy(ctx context.Context, policyID string, memberID string, memberType string, bearerToken string) error {
	profile, ok := c.currentClientProfile()
	if !ok {
		return fmt.Errorf("no current platform profile available")
	}
	policyID = strings.TrimSpace(policyID)
	memberID = strings.TrimSpace(memberID)
	memberType = strings.ToUpper(strings.TrimSpace(memberType))
	if policyID == "" {
		return fmt.Errorf("policy id is required")
	}
	if memberID == "" {
		return fmt.Errorf("member id is required")
	}
	if memberType == "" {
		return fmt.Errorf("member type is required")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/authz/v1/policies/" + url.PathEscape(policyID)
	query := u.Query()
	query.Set("policy_id", policyID)
	query.Set("member_id", memberID)
	query.Set("member_type", memberType)
	u.RawQuery = query.Encode()

	if strings.TrimSpace(bearerToken) != "" {
		return c.deleteWithBearerProfile(ctx, profile, u.String(), strings.TrimSpace(bearerToken))
	}
	_, err := c.doRequestWithProfile(ctx, profile, http.MethodDelete, u.String(), "", nil)
	return err
}

func (c *VirtualClusterClient) ResolveCurrentProfileUsernames(ctx context.Context, ids []string) (map[string]string, error) {
	profile, ok := c.currentClientProfile()
	if !ok {
		return nil, fmt.Errorf("no current platform profile available")
	}
	return c.resolveUsernamesWithProfile(ctx, profile, ids)
}

func (c *VirtualClusterClient) ResolveUsernames(ctx context.Context, ids []string) (map[string]string, error) {
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(unique))
	var lastErr error
	success := false
	queries := queryProfiles(ctx, c.orderedProfiles(), func(ctx context.Context, profile clientProfile) (map[string]string, error) {
		return c.resolveUsernamesWithProfile(ctx, profile, unique)
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, query := range queries {
		if query.err != nil {
			lastErr = query.err
			continue
		}
		success = true
		for _, id := range unique {
			if _, exists := result[id]; exists {
				continue
			}
			if value, ok := query.value[id]; ok && strings.TrimSpace(value) != "" {
				result[id] = value
			}
		}
	}
	if len(result) > 0 || success {
		return result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return map[string]string{}, nil
}

func (c *VirtualClusterClient) ResolveUserIDs(ctx context.Context, usernames []string) (map[string]string, error) {
	unique := make([]string, 0, len(usernames))
	seen := make(map[string]struct{}, len(usernames))
	for _, username := range usernames {
		username = strings.TrimSpace(username)
		key := strings.ToLower(username)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, username)
	}
	if len(unique) == 0 {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(unique))
	var lastErr error
	success := false
	queries := queryProfiles(ctx, c.orderedProfiles(), func(ctx context.Context, profile clientProfile) (map[string]string, error) {
		return c.resolveUserIDsWithProfile(ctx, profile, unique)
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, query := range queries {
		if query.err != nil {
			lastErr = query.err
			continue
		}
		success = true
		for _, username := range unique {
			key := strings.ToLower(strings.TrimSpace(username))
			if _, exists := result[key]; exists {
				continue
			}
			if id := strings.TrimSpace(query.value[key]); id != "" {
				result[key] = id
			}
		}
	}
	if len(result) > 0 || success {
		return result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return map[string]string{}, nil
}

func (c *VirtualClusterClient) SearchIAMUserGroupsForProfileToken(ctx context.Context, profileName string, searchQuery string, bearerToken string) ([]IAMUserGroupSearchItem, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	searchQuery = strings.TrimSpace(searchQuery)
	if searchQuery == "" {
		return nil, fmt.Errorf("member search query is required")
	}
	if strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("console bearer token is required")
	}

	u, _ := url.Parse(profile.IAMBaseURL)
	u.Path = "/iam/idp/v1/userGroups:search"
	query := u.Query()
	query.Set("page", "1")
	query.Set("page_size", "100")
	query.Set("query", searchQuery)
	u.RawQuery = query.Encode()

	var payload iamUserGroupSearchResponse
	if err := c.getJSONWithBearerProfile(ctx, profile, u.String(), bearerToken, &payload); err != nil {
		return nil, err
	}
	items := payload.Items
	items = append(items, payload.Users...)
	items = append(items, payload.Groups...)
	items = append(items, payload.UserGroups...)
	return items, nil
}
