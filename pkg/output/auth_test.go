package output

import (
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

func TestPrintAuthGroupResultIncludesMembersAndCollaborators(t *testing.T) {
	result := &service.AuthGroupResult{
		ID: "group-uid", Name: "ug-demo", MemberCount: 2, AdminCount: 1,
		Members: []service.AuthGroupMemberItem{
			{ID: "admin-uid", Username: "admin-user", Name: "管理员", Role: "协管员", Status: "valid", JoinTime: "2026-09-20 09:02:03", InGroup: true},
			{ID: "member-uid", Username: "member-user", Name: "普通成员", Role: "成员", Status: "valid", InGroup: true},
		},
	}
	text := captureTableOutput(t, func() { PrintAuthGroupResult(result, false) })
	for _, expected := range []string{"USERNAME", "ROLE", "admin-user", "协管员", "member-user", "成员 2 人，其中协管员 1 人"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q:\n%s", expected, text)
		}
	}
}
