package service

import "testing"

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
