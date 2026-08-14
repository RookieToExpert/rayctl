package service

import (
	"testing"

	"rayctl/internal/platform"
)

func TestResolveSSPWorkspaceRolesSupportsAliasesAndLists(t *testing.T) {
	available := []platform.IAMRoleInfo{
		{ID: "aid", RoleName: "ssp.aidCreator", DisplayName: "开发机用户"},
		{ID: "ait", RoleName: "ssp.aitOperator", DisplayName: "任务用户"},
	}
	roles, err := resolveSSPWorkspaceRoles([]string{"aid-creator,aitOperator"}, available, "/rm/workspaces/test")
	if err != nil {
		t.Fatalf("resolveSSPWorkspaceRoles() error = %v", err)
	}
	if len(roles) != 2 || roles[0].ID != "aid" || roles[1].ID != "ait" {
		t.Fatalf("resolveSSPWorkspaceRoles() = %#v", roles)
	}
}
