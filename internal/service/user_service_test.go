package service

import (
	"testing"

	"rayctl/internal/platform"
)

func TestUserGroupItemsSortsByDisplayName(t *testing.T) {
	groups := []platform.IAMGroup{
		{ID: "group-2", Name: "z-group", DisplayName: "Z Group", Status: "valid"},
		{ID: "group-1", Name: "a-group", DisplayName: "A Group", PosixGroupName: "posix-a", Status: "valid"},
	}

	items := userGroupItems(groups)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].ID != "group-1" || items[0].DisplayName != "A Group" || items[0].PosixGroupName != "posix-a" {
		t.Fatalf("first item = %+v, want A Group", items[0])
	}
	if items[1].ID != "group-2" {
		t.Fatalf("second item = %+v, want group-2", items[1])
	}
}
