package service

import (
	"testing"

	"rayctl/internal/platform"
)

func TestNormalizeNetworkGrantResourceTypes(t *testing.T) {
	tests := map[string]string{
		"vpc":         "vpc",
		"elastic-ip":  "eip",
		"nat":         "natgateway",
		"nat-gateway": "natgateway",
		"dnat":        "natgateway",
	}
	for input, want := range tests {
		if got := normalizeGrantResourceType(input); got != want {
			t.Fatalf("normalizeGrantResourceType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeNetworkGrantRoles(t *testing.T) {
	tests := []struct {
		resourceType string
		role         string
		want         string
	}{
		{resourceType: "vpc", role: "reader", want: "vpc.reader"},
		{resourceType: "vpc", role: "editor", want: "vpc.editor"},
		{resourceType: "eip", role: "reader", want: "eip.reader"},
		{resourceType: "eip", role: "editor", want: "eip.editor"},
		{resourceType: "natgateway", role: "operator", want: "246b7558-2db8-4602-9b8c-bb4464506ef6"},
	}
	for _, test := range tests {
		if got := normalizeGrantRoleName(test.resourceType, test.role); got != test.want {
			t.Fatalf("normalizeGrantRoleName(%q, %q) = %q, want %q", test.resourceType, test.role, got, test.want)
		}
	}
}

func TestNetworkGrantRoleDefinitions(t *testing.T) {
	if defs := grantRoleDefinitions("vpc"); len(defs) != 2 || defs[0].Alias != "reader" || defs[1].Alias != "editor" {
		t.Fatalf("unexpected VPC role definitions: %#v", defs)
	}
	if defs := grantRoleDefinitions("eip"); len(defs) != 2 || defs[0].RoleName != "eip.reader" || defs[1].RoleName != "eip.editor" {
		t.Fatalf("unexpected EIP role definitions: %#v", defs)
	}
	if defs := grantRoleDefinitions("natgateway"); len(defs) != 1 || defs[0].RoleID != "246b7558-2db8-4602-9b8c-bb4464506ef6" || defs[0].DisplayName != "DNAT 操作员" {
		t.Fatalf("unexpected NAT Gateway role definitions: %#v", defs)
	}
}

func TestCollectAuthGroupMembersMarksCollaborators(t *testing.T) {
	group := AuthGroupItem{ID: "group-uid", Name: "ug-demo"}
	users := []platform.IAMUser{
		{ID: "admin-uid", Username: "admin-user", Name: "管理员", Status: "valid", JoinTime: "2026-09-20T01:02:03Z"},
		{ID: "member-uid", Username: "member-user", Name: "普通成员", Status: "valid"},
	}
	policies := []platform.IAMBindingPolicy{
		{
			Scope: "/iam/groups/group-uid", MemberType: "USER", MemberValue: "admin-uid", MemberIdentify: "admin-user",
			RoleInfos: []platform.IAMRoleInfo{{RoleName: "iam.userGroupCollaborator"}},
		},
		{
			Scope: "/iam/groups/group-uid", MemberType: "USER", MemberValue: "external-admin", MemberIdentify: "external",
			RoleInfos: []platform.IAMRoleInfo{{RoleName: "iam.userGroupCollaborator"}},
		},
	}

	members, adminCount := collectAuthGroupMembers(group, users, policies)
	if adminCount != 2 || len(members) != 3 {
		t.Fatalf("members = %#v, adminCount = %d", members, adminCount)
	}
	if members[0].Role != "协管员" || members[0].Username != "admin-user" || !members[0].InGroup {
		t.Fatalf("first member = %#v", members[0])
	}
	if members[1].Role != "协管员" || members[1].Username != "external" || members[1].InGroup {
		t.Fatalf("external collaborator = %#v", members[1])
	}
	if members[2].Role != "成员" || members[2].Username != "member-user" {
		t.Fatalf("regular member = %#v", members[2])
	}
}
