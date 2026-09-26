package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestSSPWorkspaceRolesCatalogAndScopedLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/iam/authz/v1/roles":
			fmt.Fprint(w, `{"roles":[{"id":"aid","role_name":"ssp.aidCreator","display_name":"开发机用户"},{"id":"vc","role_name":"vc.admin","display_name":"VC 管理员"}]}`)
		case "/rmh/v1/resources:page":
			fmt.Fprint(w, `{"resources":[{"id":"ws-uid","name":"ws-demo","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/ws-demo"}]}`)
		case "/iam/authz/v1/roles:own":
			if got := r.URL.Query().Get("scope"); got != "/rm/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/ws-demo" {
				t.Errorf("roles scope = %q", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer token" {
				t.Errorf("Authorization = %q", got)
			}
			fmt.Fprint(w, `{"roles":[{"id":"ait","role_name":"ssp.aitOperator","display_name":"任务用户"}]}`)
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
	service := NewAuthService(client)
	catalog, err := service.GetSSPWorkspaceRoles(context.Background(), "test", "", "")
	if err != nil || len(catalog.Items) != 1 || catalog.Items[0].Alias != "aid-creator" {
		t.Fatalf("catalog = %#v, err = %v", catalog, err)
	}
	scoped, err := service.GetSSPWorkspaceRoles(context.Background(), "test", "ws-demo", "token")
	if err != nil || len(scoped.Items) != 1 || scoped.Items[0].Alias != "ait-operator" || !strings.HasSuffix(scoped.Scope, "/workspaces/ws-demo") {
		t.Fatalf("scoped = %#v, err = %v", scoped, err)
	}
}
