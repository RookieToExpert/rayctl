package output

import (
	"encoding/json"
	"strings"
	"testing"

	"rayctl/internal/service"
)

func TestDeduplicateAuthPermissions(t *testing.T) {
	items := []service.AuthPermissionItem{
		{Scope: "subscriptions/sub/resourceGroups/default/volumes/afs-shared", Roles: "Reader", PolicyID: "direct"},
		{Scope: "/rm/subscriptions/sub/resourceGroups/default/volumes/afs-shared", Roles: "Reader", PolicyID: "group-a"},
		{Scope: "subscriptions/sub/resourceGroups/default/volumes/afs-shared", Roles: "Editor", PolicyID: "group-b"},
		{Scope: "subscriptions/sub/resourceGroups/default/volumes/afs-other", Roles: "Reader", PolicyID: "group-c"},
	}

	got := deduplicateAuthPermissions(items)
	if len(got) != 3 {
		t.Fatalf("deduplicateAuthPermissions() returned %d items, want 3", len(got))
	}
	if got[0].PolicyID != "direct" {
		t.Fatalf("first matching permission should be preserved, got policy %q", got[0].PolicyID)
	}
}

func TestDeduplicateAuthPermissionsIgnoresRoleOrder(t *testing.T) {
	items := []service.AuthPermissionItem{
		{Scope: "tenant", Roles: "Reader,Editor"},
		{Scope: "tenant", Roles: "Editor, Reader"},
	}

	if got := deduplicateAuthPermissions(items); len(got) != 1 {
		t.Fatalf("deduplicateAuthPermissions() returned %d items, want 1", len(got))
	}
}

func TestCompactAuthPermissionsMergesRolesByScope(t *testing.T) {
	items := []service.AuthPermissionItem{
		{Scope: "/rm/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a", Roles: "Viewer"},
		{Scope: "/rm/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a", Roles: "Admin"},
		{Scope: "/rm/subscriptions/sub/resourceGroups/default", Roles: "Resource Viewer"},
	}

	got := compactAuthPermissions(items)
	if len(got) != 2 {
		t.Fatalf("compactAuthPermissions() returned %d items, want 2", len(got))
	}
	if got[0].Roles != "Admin, Viewer" {
		t.Fatalf("merged roles = %q, want Admin, Viewer", got[0].Roles)
	}
}

func TestFormatAuthScopeForUserDisplayResolvesGroupName(t *testing.T) {
	groups := []service.AuthGroupItem{{
		ID:          "group-id",
		Name:        "ug-platform",
		DisplayName: "平台组",
	}}
	if got := formatAuthScopeForUserDisplay("iam/groups/group-id", groups); got != "用户组 平台组" {
		t.Fatalf("group scope = %q, want 用户组 平台组", got)
	}
	if got := formatAuthScopeForUserDisplay("iam/groups/unknown", groups); got != "用户组 unknown" {
		t.Fatalf("unknown group scope = %q, want 用户组 unknown", got)
	}
}

func TestPrintGroupGetResultIncludesMembersAndCollaborators(t *testing.T) {
	result := &service.GroupGetResult{
		ID: "group-uid", Name: "ug-demo", MemberCount: 2, AdminCount: 1,
		Members: []service.AuthGroupMemberItem{
			{ID: "admin-uid", Username: "admin-user", Name: "管理员", Role: "协管员", Status: "valid", JoinTime: "2026-09-20 09:02:03", InGroup: true},
			{ID: "member-uid", Username: "member-user", Name: "普通成员", Role: "成员", Status: "valid", InGroup: true},
		},
	}
	text := captureTableOutput(t, func() { PrintGroupGetResult(result, false) })
	for _, expected := range []string{"USERNAME", "ROLE", "admin-user", "协管员", "member-user", "成员 2 人，其中协管员 1 人"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q:\n%s", expected, text)
		}
	}
}

func TestGroupMetadataOnlyAppearsInLongOutputWhenPresent(t *testing.T) {
	group := &service.GroupGetResult{ID: "group-uid", Name: "ug-demo", PosixGroupName: "posix-demo", TenantID: "tenant-uid"}
	short := captureTableOutput(t, func() { PrintGroupGetResult(group, false) })
	if strings.Contains(short, "POSIX") || strings.Contains(short, "TENANT") {
		t.Fatalf("short group output contains optional metadata:\n%s", short)
	}
	long := captureTableOutput(t, func() { PrintGroupGetResult(group, true) })
	for _, expected := range []string{"POSIX", "posix-demo", "TENANT ID", "tenant-uid"} {
		if !strings.Contains(long, expected) {
			t.Fatalf("long group output missing %q:\n%s", expected, long)
		}
	}
	if strings.Contains(long, "TENANT CODE") {
		t.Fatalf("long group output contains unavailable fields:\n%s", long)
	}
	group.PosixGroupName = ""
	group.TenantID = ""
	empty := captureTableOutput(t, func() { PrintGroupGetResult(group, true) })
	if strings.Contains(empty, "POSIX") || strings.Contains(empty, "TENANT ID") {
		t.Fatalf("long group output contains empty fields:\n%s", empty)
	}
}

func TestDirectoryListsKeepDetailsInLongOutput(t *testing.T) {
	users := []service.UserListItem{{ID: "user-id", Username: "tester", Name: "Tester", TenantCode: "tenant-code", Status: "valid"}}
	groups := []service.GroupListItem{{ID: "group-id", Name: "ug-demo", TenantID: "tenant-id", PosixGroupName: "posix-demo"}}
	for _, test := range []struct {
		name   string
		short  func()
		long   func()
		marker string
	}{
		{"user", func() { PrintUserList(users, false) }, func() { PrintUserList(users, true) }, "tenant-code"},
		{"group", func() { PrintGroupList(groups, false) }, func() { PrintGroupList(groups, true) }, "tenant-id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			short := captureTableOutput(t, test.short)
			long := captureTableOutput(t, test.long)
			if strings.Contains(short, test.marker) || !strings.Contains(long, test.marker) {
				t.Fatalf("unexpected short/long metadata: short=%q long=%q", short, long)
			}
		})
	}
}

func TestPrintAuthGroupResultShowsOnlyPermissions(t *testing.T) {
	result := &service.AuthGroupResult{
		ID: "group-uid", Name: "ug-demo",
		Permissions: []service.AuthPermissionItem{{Scope: "/rm/subscriptions/sub", Roles: "Reader"}},
	}
	for _, render := range []func(){
		func() { PrintAuthGroupResult(result, false) },
		func() { PrintAuthGroupResult(result, true) },
		func() { PrintAuthGroupSummary([]*service.AuthGroupResult{result}) },
	} {
		text := captureTableOutput(t, render)
		if !strings.Contains(text, "Reader") || strings.Contains(text, "成员") || strings.Contains(text, "协管员") {
			t.Fatalf("auth group output should only show permissions:\n%s", text)
		}
	}
}

func TestGroupAndAuthJSONContainOnlyTheirOwnData(t *testing.T) {
	auth := &service.AuthGroupResult{ID: "group-uid", Permissions: []service.AuthPermissionItem{{Roles: "Reader"}}}
	group := &service.GroupGetResult{ID: "group-uid", Members: []service.AuthGroupMemberItem{{Username: "tester"}}, MemberCount: 1}
	for _, test := range []struct {
		name   string
		render func()
		want   string
		omit   string
	}{
		{"auth", func() { PrintAuthGroupSummary([]*service.AuthGroupResult{auth}) }, "permissions", "members"},
		{"group", func() { PrintGroupGetSummary([]*service.GroupGetResult{group}, false) }, "members", "permissions"},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := BeginJSON()
			defer EndJSON()
			test.render()
			body, err := json.Marshal(session.Items)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), test.want) || strings.Contains(string(body), test.omit) {
				t.Fatalf("JSON = %s", body)
			}
		})
	}
}
