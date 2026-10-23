package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"rayctl/internal/platform"
)

func TestAuthUserRetainsMatchingMemberOfSharedPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/iam/idp/v1/getUsers":
			fmt.Fprint(w, `{"users":[{"id":"user-id","username":"tester","name":"Test User","tenant_code":"tenant","source":"local"}]}`)
		case "/iam/idp/v1/users/user-id:getGroups":
			fmt.Fprint(w, `{"groups":[{"id":"member-group","name":"member-group-name","display_name":"Member Group","posix_group_name":"member-posix"}]}`)
		case "/iam/authz/v1/bindingPolicies":
			// The API flattens policy members into rows sharing one policy ID.
			// An unrelated member can arrive before the user's group.
			fmt.Fprint(w, `{"policies":[{"id":"shared-policy","scope":"/rm/shared","member_type":"GROUP","member_value":"unrelated-group","role_infos":[{"role_name":"reader","display_name":"Reader"}]},{"id":"shared-policy","scope":"/rm/shared","member_type":"GROUP","member_value":"member-group","role_infos":[{"role_name":"reader","display_name":"Reader"}]}],"total_size":2}`)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "platform.json")
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","base_url":%q,"iam_base_url":%q}}}`, server.URL, server.URL)
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	results, err := NewAuthService(client).GetUserForProfile(context.Background(), "tester", "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Permissions) != 1 {
		t.Fatalf("auth user result = %#v, want one matching permission", results)
	}
	permission := results[0].Permissions[0]
	if permission.PolicyID != "shared-policy" || permission.Member != "Member Group" || permission.Scope != "/shared" || permission.RoleNames != "reader" {
		t.Fatalf("matching permission = %#v", permission)
	}
}

func TestCollectUserPermissionsKeepsEveryMatchingSharedPolicyMember(t *testing.T) {
	groups := map[string]AuthGroupItem{
		"group-a": {ID: "group-a", DisplayName: "Group A"},
		"group-b": {ID: "group-b", DisplayName: "Group B"},
	}
	policies := []platform.IAMBindingPolicy{
		{ID: "shared-policy", Scope: "/rm/shared", MemberType: "GROUP", MemberValue: "group-a"},
		{ID: "shared-policy", Scope: "/rm/shared", MemberType: "GROUP", MemberValue: "group-b"},
	}
	permissions := collectUserPermissions(platform.IAMUser{ID: "user-id"}, groups, policies)
	if len(permissions) != 2 || permissions[0].Member != "Group A" || permissions[1].Member != "Group B" {
		t.Fatalf("shared policy permissions = %#v, want both member groups", permissions)
	}
}
