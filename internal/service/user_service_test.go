package service

import (
	"reflect"
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

func TestUserWorkloadOptionsAny(t *testing.T) {
	if (UserWorkloadOptions{}).Any() {
		t.Fatal("empty workload options reported enabled")
	}
	if !(UserWorkloadOptions{AIT: true}).Any() {
		t.Fatal("AIT workload option reported disabled")
	}
}

func TestFilterUserWorkloadsMatchesCreatorNameOrID(t *testing.T) {
	items := []UserWorkloadItem{
		{Type: "AID", Name: "by-name", Creator: "tester"},
		{Type: "AIT", Name: "by-id", Creator: "display-name", CreatorID: "user-1"},
		{Type: "AIR", Name: "other", Creator: "someone-else"},
	}
	identities := userIdentitySet([]platform.IAMUser{{ID: "user-1", Username: "tester"}})
	got := filterUserWorkloads(items, identities)
	want := []string{"by-name", "by-id"}
	names := make([]string, 0, len(got))
	for _, item := range got {
		names = append(names, item.Name)
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("filtered workloads = %v, want %v", names, want)
	}
}

func TestParseUserWorkloadTime(t *testing.T) {
	got := parseUserWorkloadTime("2026-09-14 12:34:56")
	if got.IsZero() || got.In(utcPlus8).Format("2006-01-02 15:04:05") != "2026-09-14 12:34:56" {
		t.Fatalf("parsed workload time = %v", got)
	}
	if got.Location().String() != utcPlus8.String() {
		t.Fatalf("location = %s, want %s", got.Location(), utcPlus8)
	}
	if !parseUserWorkloadTime("invalid").IsZero() {
		t.Fatal("invalid workload time should remain zero")
	}
}

func TestUserWorkspacesMatchesEnvironmentPrefixedGroups(t *testing.T) {
	users := []platform.IAMUser{{ID: "user-1"}}
	groups := map[string][]platform.IAMGroup{
		"user-1": {
			{Name: "ug-p-sciverse-agent"},
			{Name: "ug-a3-infra"},
		},
	}
	workspaces := []platform.SSPWorkspace{
		{Name: "ws-p-sciverse-agent"},
		{Name: "ws-d-a3-infra"},
		{Name: "ws-d-other"},
	}
	got := userWorkspaces(workspaces, users, groups)
	if len(got) != 2 || got[0].Name != "ws-p-sciverse-agent" || got[1].Name != "ws-d-a3-infra" {
		t.Fatalf("matched workspaces = %#v", got)
	}
}

func TestUserWorkspacesFallsBackWhenGroupsDoNotMap(t *testing.T) {
	users := []platform.IAMUser{{ID: "user-1"}}
	groups := map[string][]platform.IAMGroup{"user-1": {{Name: "custom-group"}}}
	workspaces := []platform.SSPWorkspace{{Name: "ws-d-a"}, {Name: "ws-p-b"}}
	got := userWorkspaces(workspaces, users, groups)
	if len(got) != len(workspaces) {
		t.Fatalf("fallback workspaces = %#v, want all", got)
	}
}
