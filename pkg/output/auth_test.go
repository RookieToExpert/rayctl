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
