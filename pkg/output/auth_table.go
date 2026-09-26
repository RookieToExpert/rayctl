package output

import (
	"fmt"
	"os"
	"rayctl/internal/service"
	"sort"
	"strconv"
	"strings"
)

func PrintAuthAFSResult(result *service.AuthAFSResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}

	resourceRows := [][]string{
		{"RESOURCE TYPE", emptyDash(result.ResourceType)},
		{"RESOURCE", emptyDash(firstNonEmptyOutput(result.ResourceName, result.AFSName))},
	}
	if strings.EqualFold(strings.TrimSpace(result.ResourceType), "AFS") || strings.TrimSpace(result.ResourceType) == "" {
		resourceRows = [][]string{{"AFS", emptyDash(firstNonEmptyOutput(result.AFSName, result.ResourceName))}}
	}
	summaryRows := append(resourceRows, [][]string{
		{"SCOPE", emptyDash(formatAuthScopeForDisplay(result.Scope))},
		{"授权数量", strconv.Itoa(len(result.Items))},
	}...)

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		summaryRows,
		[]int{14, 64},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(summaryRows), 1)...),
			minWidths:   []int{10, 20},
		},
	)

	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	if len(result.Items) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-"})
	} else {
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.MemberType),
				emptyDash(item.MemberName),
				emptyDash(item.MemberIdentify),
				emptyDash(item.MemberValue),
				emptyDash(item.Roles),
				emptyDash(item.RoleNames),
				emptyDash(item.CreateTime),
			})
		}
	}

	noWrapCells := make([][2]int, 0, len(rows)*2)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 0)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 6)...)
	printBoxTableWithOptions(
		[]string{"TYPE", "NAME", "IDENTIFY", "ID", "ROLES", "ROLE NAMES", "CREATE TIME"},
		rows,
		[]int{8, 28, 28, 36, 36, 36, 19},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{6, 10, 10, 18, 12, 12, 19},
		},
	)
}

func PrintAuthResourceSummary(results []*service.AuthAFSResult, includeResource bool) {
	if captureJSON(results) {
		return
	}
	rows := make([][]string, 0)
	for _, result := range results {
		resource := firstNonEmptyOutput(result.ResourceName, result.AFSName)
		for _, item := range result.Items {
			member := firstNonEmptyOutput(item.MemberName, item.MemberIdentify, item.MemberValue)
			row := []string{emptyDash(member), emptyDash(firstNonEmptyOutput(item.Roles, item.RoleNames))}
			if includeResource {
				row = append([]string{emptyDash(resource)}, row...)
			}
			rows = append(rows, row)
		}
	}
	headers := []string{"MEMBER", "ROLES"}
	widths := []int{36, 48}
	minimums := []int{14, 16}
	if includeResource {
		headers = append([]string{"RESOURCE"}, headers...)
		widths = append([]int{32}, widths...)
		minimums = append([]int{16}, minimums...)
	}
	if len(rows) == 0 {
		row := []string{"-", "-"}
		if includeResource {
			row = append([]string{"-"}, row...)
		}
		rows = append(rows, row)
	}
	printBoxTableWithOptions(headers, rows, widths, tableOptions{minWidths: minimums})
}

func PrintAuthUserSummary(results []*service.AuthUserResult) {
	if captureJSON(results) {
		return
	}
	if len(results) == 0 {
		printAuthUserIdentityTable(nil)
		printBoxTableWithOptions([]string{"SCOPE", "ROLES"}, [][]string{{"-", "-"}}, []int{56, 64}, tableOptions{minWidths: []int{18, 12}})
		return
	}
	type userGroup struct {
		result  *service.AuthUserResult
		results []*service.AuthUserResult
	}
	groups := make([]userGroup, 0)
	indexes := map[string]int{}
	for _, result := range results {
		key := strings.ToLower(firstNonEmptyOutput(result.Username, result.ID))
		index, ok := indexes[key]
		if !ok {
			copyResult := *result
			copyResult.Groups = nil
			groups = append(groups, userGroup{result: &copyResult})
			index = len(groups) - 1
			indexes[key] = index
		}
		groups[index].results = append(groups[index].results, result)
		seenGroups := map[string]struct{}{}
		for _, item := range groups[index].result.Groups {
			seenGroups[strings.ToLower(firstNonEmptyOutput(item.ID, item.Name, item.PosixGroupName))] = struct{}{}
		}
		for _, item := range result.Groups {
			groupKey := strings.ToLower(firstNonEmptyOutput(item.ID, item.Name, item.PosixGroupName))
			if _, exists := seenGroups[groupKey]; exists {
				continue
			}
			groups[index].result.Groups = append(groups[index].result.Groups, item)
			seenGroups[groupKey] = struct{}{}
		}
	}
	for resultIndex, group := range groups {
		if resultIndex > 0 {
			fmt.Fprintln(os.Stdout)
		}
		identities := make([]string, 0, len(group.results))
		seenIdentities := map[string]struct{}{}
		for _, result := range group.results {
			identity := emptyDash(result.ID)
			if len(group.results) > 1 {
				identity = emptyDash(result.ProfileName) + "=" + identity
			}
			if _, exists := seenIdentities[identity]; exists {
				continue
			}
			seenIdentities[identity] = struct{}{}
			identities = append(identities, identity)
		}
		group.result.ID = strings.Join(identities, "\n")
		printAuthUserIdentityTable(group.result)
		rows := make([][]string, 0)
		seen := map[string]struct{}{}
		for _, result := range group.results {
			permissions := compactAuthPermissions(result.Permissions)
			if len(permissions) == 0 {
				rows = append(rows, []string{emptyDash(result.ProfileName), emptyDash(strings.ToUpper(result.Environment)), "-", "-"})
				continue
			}
			for _, item := range permissions {
				row := []string{emptyDash(result.ProfileName), emptyDash(strings.ToUpper(result.Environment)), emptyDash(formatAuthScopeForUserDisplay(item.Scope, result.Groups)), emptyDash(item.Roles)}
				key := strings.ToLower(strings.Join(row, "\x00"))
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				rows = append(rows, row)
			}
		}
		printBoxTableWithOptions([]string{"TENANT", "ENV", "SCOPE", "ROLES"}, rows, []int{24, 6, 50, 58}, tableOptions{minWidths: []int{12, 5, 18, 12}})
	}
}

func printAuthUserIdentityTable(result *service.AuthUserResult) {
	username := "-"
	id := "-"
	groups := "-"
	if result != nil {
		username = emptyDash(result.Username)
		id = emptyDash(result.ID)
		groups = userGroupNames(result.Groups)
	}
	printBoxTableWithOptions(
		[]string{"USER", "ID", "GROUPS"},
		[][]string{{username, id, groups}},
		[]int{24, 58, 48},
		tableOptions{
			noWrapCells: makeNoWrapCells([2]int{0, 1}),
			minWidths:   []int{12, 58, 16},
		},
	)
}

func PrintAuthGroupSummary(results []*service.AuthGroupResult) {
	if captureJSON(results) {
		return
	}
	if len(results) == 0 {
		printAuthIdentityTable("GROUP", "-", "-")
		printBoxTableWithOptions([]string{"SCOPE", "ROLES"}, [][]string{{"-", "-"}}, []int{56, 64}, tableOptions{minWidths: []int{18, 12}})
		return
	}
	for resultIndex, result := range results {
		if resultIndex > 0 {
			fmt.Fprintln(os.Stdout)
		}
		name := firstNonEmptyOutput(result.DisplayName, result.Name, result.PosixGroupName)
		printAuthIdentityTable("GROUP", name, result.ID)
		permissions := compactAuthPermissions(result.Permissions)
		rows := make([][]string, 0, maxInt(1, len(permissions)))
		if len(permissions) == 0 {
			rows = append(rows, []string{"-", "-"})
		} else {
			for _, item := range permissions {
				group := service.AuthGroupItem{ID: result.ID, Name: result.Name, DisplayName: result.DisplayName, PosixGroupName: result.PosixGroupName}
				rows = append(rows, []string{emptyDash(formatAuthScopeForUserDisplay(item.Scope, []service.AuthGroupItem{group})), emptyDash(item.Roles)})
			}
		}
		printBoxTableWithOptions([]string{"SCOPE", "ROLES"}, rows, []int{56, 64}, tableOptions{minWidths: []int{18, 12}})
	}
}

func printAuthIdentityTable(kind string, name string, id string) {
	printBoxTableWithOptions(
		[]string{kind, "ID"},
		[][]string{{emptyDash(name), emptyDash(id)}},
		[]int{32, 36},
		tableOptions{minWidths: []int{12, 36}},
	)
}

func formatAuthScopeForUserDisplay(scope string, groups []service.AuthGroupItem) string {
	normalized := strings.Trim(strings.TrimPrefix(strings.TrimSpace(scope), "/rm"), "/")
	parts := strings.Split(normalized, "/")
	if len(parts) == 3 && strings.EqualFold(parts[0], "iam") && strings.EqualFold(parts[1], "groups") {
		groupID := strings.TrimSpace(parts[2])
		for _, group := range groups {
			if strings.EqualFold(strings.TrimSpace(group.ID), groupID) {
				return "用户组 " + firstNonEmptyOutput(group.DisplayName, group.Name, group.PosixGroupName, group.ID)
			}
		}
		return "用户组 " + groupID
	}
	return formatAuthScopeForDisplay(scope)
}

func PrintAuthUserResult(result *service.AuthUserResult, long bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	permissions := result.Permissions
	if !long {
		permissions = deduplicateAuthPermissions(permissions)
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"PROFILE", emptyDash(result.ProfileName)},
			{"ENVIRONMENT", emptyDash(strings.ToUpper(result.Environment))},
			{"ID", emptyDash(result.ID)},
			{"USERNAME", emptyDash(result.Username)},
			{"NAME", emptyDash(result.Name)},
			{"TENANT CODE", emptyDash(result.TenantCode)},
			{"STATUS", emptyDash(result.Status)},
			{"SOURCE", emptyDash(result.Source)},
			{"用户组数量", strconv.Itoa(len(result.Groups))},
			{"权限数量", strconv.Itoa(len(permissions))},
		},
		[]int{14, 72},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
				[2]int{4, 1},
				[2]int{5, 1},
				[2]int{6, 1},
				[2]int{7, 1},
				[2]int{8, 1},
				[2]int{9, 1},
			),
			minWidths: []int{10, 20},
		},
	)

	fmt.Fprintln(os.Stdout)
	permissionRows := make([][]string, 0, maxInt(1, len(permissions)))
	if !long {
		if len(permissions) == 0 {
			permissionRows = append(permissionRows, []string{"-", "-"})
		} else {
			for _, item := range permissions {
				permissionRows = append(permissionRows, []string{
					emptyDash(formatAuthScopeForDisplay(item.Scope)),
					emptyDash(item.Roles),
				})
			}
		}
		printBoxTableWithOptions(
			[]string{"SCOPE", "ROLES"},
			permissionRows,
			[]int{56, 36},
			tableOptions{
				minWidths: []int{18, 10},
			},
		)
		return
	}

	groupRows := make([][]string, 0, maxInt(1, len(result.Groups)))
	if len(result.Groups) == 0 {
		groupRows = append(groupRows, []string{"-", "-", "-", "-"})
	} else {
		for _, group := range result.Groups {
			groupRows = append(groupRows, []string{
				emptyDash(firstNonEmptyOutput(group.DisplayName, group.Name, group.PosixGroupName)),
				emptyDash(group.PosixGroupName),
				emptyDash(group.ID),
				emptyDash(group.Status),
			})
		}
	}
	groupNoWrap := make([][2]int, 0, len(groupRows))
	groupNoWrap = append(groupNoWrap, noWrapCellsForSingleColumn(len(groupRows), 3)...)
	printBoxTableWithOptions(
		[]string{"GROUP", "POSIX", "ID", "STATUS"},
		groupRows,
		[]int{32, 28, 36, 10},
		tableOptions{
			noWrapCells: makeNoWrapCells(groupNoWrap...),
			minWidths:   []int{10, 10, 18, 8},
		},
	)

	fmt.Fprintln(os.Stdout)
	permissionRows = make([][]string, 0, maxInt(1, len(result.Permissions)))
	if len(result.Permissions) == 0 {
		permissionRows = append(permissionRows, []string{"-", "-", "-", "-", "-", "-", "-"})
	} else {
		for _, item := range result.Permissions {
			permissionRows = append(permissionRows, []string{
				emptyDash(item.Source),
				emptyDash(item.Member),
				emptyDash(item.Service),
				emptyDash(formatAuthScopeForDisplay(item.Scope)),
				emptyDash(item.Roles),
				emptyDash(item.RoleNames),
				emptyDash(item.CreateTime),
			})
		}
	}
	permissionNoWrap := make([][2]int, 0, len(permissionRows)*3)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 0)...)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 2)...)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 6)...)
	printBoxTableWithOptions(
		[]string{"SOURCE", "MEMBER", "SERVICE", "SCOPE", "ROLES", "ROLE NAMES", "CREATE TIME"},
		permissionRows,
		[]int{8, 24, 10, 56, 28, 28, 19},
		tableOptions{
			noWrapCells: makeNoWrapCells(permissionNoWrap...),
			minWidths:   []int{6, 10, 8, 18, 10, 12, 19},
		},
	)
}

func formatAuthScopeForDisplay(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" || scope == "-" {
		return scope
	}
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

func deduplicateAuthPermissions(items []service.AuthPermissionItem) []service.AuthPermissionItem {
	result := make([]service.AuthPermissionItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		scope := strings.Trim(strings.TrimPrefix(strings.TrimSpace(item.Scope), "/rm"), "/")
		roles := normalizedAuthRoles(item.Roles)
		if roles == "" {
			roles = normalizedAuthRoles(item.RoleNames)
		}
		key := scope + "\x00" + roles
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func compactAuthPermissions(items []service.AuthPermissionItem) []service.AuthPermissionItem {
	deduplicated := deduplicateAuthPermissions(items)
	result := make([]service.AuthPermissionItem, 0, len(deduplicated))
	indexByScope := make(map[string]int, len(deduplicated))
	rolesByScope := make(map[string]map[string]struct{}, len(deduplicated))
	for _, item := range deduplicated {
		scope := strings.Trim(strings.TrimPrefix(strings.TrimSpace(item.Scope), "/rm"), "/")
		index, exists := indexByScope[scope]
		if !exists {
			index = len(result)
			indexByScope[scope] = index
			result = append(result, item)
			rolesByScope[scope] = make(map[string]struct{})
		}
		roles := strings.Split(firstNonEmptyOutput(item.Roles, item.RoleNames), ",")
		for _, role := range roles {
			if role = strings.TrimSpace(role); role != "" {
				rolesByScope[scope][role] = struct{}{}
			}
		}
		mergedRoles := make([]string, 0, len(rolesByScope[scope]))
		for role := range rolesByScope[scope] {
			mergedRoles = append(mergedRoles, role)
		}
		sort.Strings(mergedRoles)
		result[index].Roles = strings.Join(mergedRoles, ", ")
	}
	return result
}

func normalizedAuthRoles(value string) string {
	parts := strings.Split(value, ",")
	roles := make([]string, 0, len(parts))
	for _, part := range parts {
		if role := strings.TrimSpace(part); role != "" {
			roles = append(roles, role)
		}
	}
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

func PrintAuthGroupResult(result *service.AuthGroupResult, long bool) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	permissions := result.Permissions
	if !long {
		permissions = deduplicateAuthPermissions(permissions)
	}
	fields := [][]string{
		{"ID", emptyDash(result.ID)},
		{"NAME", emptyDash(result.Name)},
		{"DISPLAY NAME", emptyDash(result.DisplayName)},
	}
	if long {
		if result.PosixGroupName != "" {
			fields = append(fields, []string{"POSIX", result.PosixGroupName})
		}
		if result.TenantID != "" {
			fields = append(fields, []string{"TENANT ID", result.TenantID})
		}
		if result.Status != "" {
			fields = append(fields, []string{"STATUS", result.Status})
		}
	}
	fields = append(fields, []string{"权限数量", strconv.Itoa(len(permissions))})

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		fields,
		[]int{14, 72},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(fields), 1)...),
			minWidths:   []int{10, 20},
		},
	)

	fmt.Fprintln(os.Stdout)
	permissionRows := make([][]string, 0, maxInt(1, len(permissions)))
	if !long {
		if len(permissions) == 0 {
			permissionRows = append(permissionRows, []string{"-", "-"})
		} else {
			for _, item := range permissions {
				permissionRows = append(permissionRows, []string{
					emptyDash(formatAuthScopeForDisplay(item.Scope)),
					emptyDash(item.Roles),
				})
			}
		}
		printBoxTableWithOptions(
			[]string{"SCOPE", "ROLES"},
			permissionRows,
			[]int{56, 36},
			tableOptions{
				minWidths: []int{18, 10},
			},
		)
		return
	}

	if len(result.Permissions) == 0 {
		permissionRows = append(permissionRows, []string{"-", "-", "-", "-", "-", "-"})
	} else {
		for _, item := range result.Permissions {
			permissionRows = append(permissionRows, []string{
				emptyDash(item.Source),
				emptyDash(item.Service),
				emptyDash(formatAuthScopeForDisplay(item.Scope)),
				emptyDash(item.Roles),
				emptyDash(item.RoleNames),
				emptyDash(item.CreateTime),
			})
		}
	}
	permissionNoWrap := make([][2]int, 0, len(permissionRows)*2)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 0)...)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 1)...)
	permissionNoWrap = append(permissionNoWrap, noWrapCellsForSingleColumn(len(permissionRows), 5)...)
	printBoxTableWithOptions(
		[]string{"SOURCE", "SERVICE", "SCOPE", "ROLES", "ROLE NAMES", "CREATE TIME"},
		permissionRows,
		[]int{8, 10, 56, 28, 28, 19},
		tableOptions{
			noWrapCells: makeNoWrapCells(permissionNoWrap...),
			minWidths:   []int{6, 8, 18, 10, 12, 19},
		},
	)
}

func PrintAuthGrantAFSResult(result *service.AuthGrantAFSResult) {
	if result == nil {
		return
	}

	resourceRows := [][]string{
		{"RESOURCE TYPE", emptyDash(result.ResourceType)},
		{"RESOURCE", emptyDash(firstNonEmptyOutput(result.ResourceName, result.AFSName))},
	}
	if strings.EqualFold(strings.TrimSpace(result.ResourceType), "AFS") || strings.TrimSpace(result.ResourceType) == "" {
		resourceRows = [][]string{{"AFS", emptyDash(firstNonEmptyOutput(result.AFSName, result.ResourceName))}}
	}
	rows := append(resourceRows, [][]string{
		{"SCOPE", emptyDash(result.Scope)},
		{"MEMBER TYPE", emptyDash(result.MemberType)},
		{"MEMBER NAME", emptyDash(result.MemberName)},
		{"MEMBER IDENTIFY", emptyDash(result.MemberIdentify)},
		{"MEMBER ID", emptyDash(result.MemberValue)},
		{"ROLE", emptyDash(result.RoleName)},
		{"ROLE ID", emptyDash(result.RoleID)},
		{"RESULT", emptyDash(result.Result)},
		{"POLICY ID", emptyDash(result.PolicyID)},
	}...)

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{16, 104},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{2, 1},
				[2]int{4, 1},
				[2]int{5, 1},
				[2]int{6, 1},
				[2]int{7, 1},
				[2]int{8, 1},
				[2]int{9, 1},
			),
			minWidths: []int{12, 28},
		},
	)

	if strings.TrimSpace(result.Payload) == "" {
		return
	}
	fmt.Fprintln(os.Stdout)
	printBoxTableWithOptions(
		[]string{"PAYLOAD"},
		[][]string{{result.Payload}},
		[]int{120},
		tableOptions{
			minWidths: []int{40},
		},
	)
}

func PrintAuthRolesResult(result *service.AuthRolesResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}

	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	if len(result.Items) == 0 {
		rows = append(rows, []string{"-", "-", "-"})
	} else {
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.Alias),
				emptyDash(item.DisplayName),
				emptyDash(item.Description),
			})
		}
	}
	noWrapCells := make([][2]int, 0, len(rows))
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 0)...)
	printBoxTableWithOptions(
		[]string{"ALIAS", "DISPLAY NAME", "DESCRIPTION"},
		rows,
		[]int{16, 32, 64},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{8, 12, 18},
		},
	)
}

func PrintAuthSSPResult(result *service.AuthSSPResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}
	rows := make([][]string, 0, len(result.Items))
	for _, item := range result.Items {
		rows = append(rows, []string{
			emptyDash(item.Type),
			emptyDash(firstNonEmptyOutput(item.Name, item.DisplayName)),
			emptyDash(item.Priority),
			emptyDash(item.Roles),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-"})
	}
	printBoxTableWithOptions(
		[]string{"TYPE", "MEMBER", "PRIORITY", "ROLES"},
		rows,
		[]int{8, 32, 10, 72},
		tableOptions{minWidths: []int{6, 12, 8, 20}, rowDividers: true},
	)
}

func PrintAuthSSPGrantResult(result *service.AuthSSPGrantResult) {
	if result == nil {
		return
	}
	rows := [][]string{
		{"WORKSPACE", emptyDash(result.Workspace)},
		{"PROFILE", emptyDash(result.ProfileName)},
		{"MEMBER TYPE", emptyDash(result.MemberType)},
		{"MEMBER", emptyDash(result.MemberName)},
		{"MEMBER ID", emptyDash(result.MemberID)},
		{"PRIORITY", emptyDash(result.Priority)},
		{"ROLES", emptyDash(result.Roles)},
		{"RESULT", emptyDash(result.Result)},
	}
	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		rows,
		[]int{14, 96},
		tableOptions{minWidths: []int{10, 24}},
	)
	if strings.TrimSpace(result.Payload) != "" {
		fmt.Fprintln(os.Stdout)
		printBoxTableWithOptions([]string{"PAYLOAD"}, [][]string{{result.Payload}}, []int{112}, tableOptions{minWidths: []int{40}})
	}
}
