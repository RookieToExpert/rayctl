package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"rayctl/internal/platform"
)

func TestGroupPermissionsDoNotLoadMembers(t *testing.T) {
	var memberRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/iam/idp/v1/groups":
			fmt.Fprint(w, `{"groups":[{"id":"group-uid","name":"ug-demo","tenant_id":"tenant-uid","posix_group_name":"posix-demo"}]}`)
		case "/iam/authz/v1/bindingPolicies":
			fmt.Fprint(w, `{"policies":[{"id":"policy-1","member_type":"GROUP","member_value":"group-uid","scope":"/rm/subscriptions/sub","role_infos":[{"role_name":"reader"}]},{"id":"policy-2","member_type":"USER","member_value":"user-uid","scope":"/iam/groups/group-uid","role_infos":[{"role_name":"iam.userGroupCollaborator"}]}]}`)
		case "/iam/idp/v1/groups/group-uid:getUsers":
			memberRequests.Add(1)
			fmt.Fprint(w, `{"users":[{"id":"user-uid","username":"tester","status":"valid"}]}`)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "platform.json")
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","iam_base_url":%q}}}`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	service := NewAuthService(client)
	permissions, err := service.GetGroupsForProfile(context.Background(), "ug-demo", "test")
	if err != nil || len(permissions) != 1 || len(permissions[0].Permissions) != 1 {
		t.Fatalf("permissions = %#v, err = %v", permissions, err)
	}
	if memberRequests.Load() != 0 {
		t.Fatal("auth group query loaded members")
	}
	members, err := service.GetGroupMembersForProfile(context.Background(), "ug-demo", "test")
	if err != nil || len(members) != 1 || len(members[0].Members) != 1 || members[0].Members[0].Role != "协管员" || members[0].MemberCount != 1 || members[0].AdminCount != 1 {
		t.Fatalf("members = %#v, err = %v", members, err)
	}
	if memberRequests.Load() != 1 {
		t.Fatalf("member requests = %d, want 1", memberRequests.Load())
	}
	if members[0].TenantID != "tenant-uid" || members[0].PosixGroupName != "posix-demo" {
		t.Fatalf("group metadata = %#v", members[0])
	}
	if permissions[0].TenantID != "tenant-uid" {
		t.Fatalf("auth group tenant ID = %q", permissions[0].TenantID)
	}
	if strings.Contains(fmt.Sprint(members), "Permissions") {
		t.Fatal("group get result includes permissions")
	}
}
