package output

import (
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
