package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rayctl/internal/platform"
)

func TestAuthGrantChecksOnlyTargetScopeBindings(t *testing.T) {
	const scope = "/subscriptions/sub/resourceGroups/default/regions/d/volumes/afs-target"
	for _, existingScope := range []string{"", scope, "/rm" + scope} {
		name := existingScope
		if name == "" {
			name = "new grant"
		}
		t.Run(name, func(t *testing.T) {
			policyCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("dry-run attempted %s %s", r.Method, r.URL.Path)
					http.Error(w, "write forbidden", http.StatusMethodNotAllowed)
					return
				}
				switch r.URL.Path {
				case "/iam/idp/v1/getUsers", "/iam/idp/v1/users":
					fmt.Fprint(w, `{"users":[{"id":"user-id","username":"tester","name":"Test User","tenant_code":"tenant","source":"local"}],"total_size":1}`)
				case "/iam/authz/v1/roles":
					fmt.Fprint(w, `{"roles":[{"id":"role-id","role_name":"afs.volumeOwner"}],"total_size":1}`)
				case "/iam/authz/v1/bindingPolicies":
					policyCalls++
					if !strings.Contains(r.URL.Query().Get("filter"), `scope="*`+scope+`*"`) {
						t.Errorf("grant queried tenant-wide bindings: %s", r.URL)
					}
					rows := []platform.IAMBindingPolicy{{
						ID: "shared-policy", Scope: "/rm" + scope, MemberType: "USER", MemberValue: "another-user",
						RoleInfos: []platform.IAMRoleInfo{{ID: "role-id", RoleName: "afs.volumeOwner"}},
					}}
					if existingScope != "" {
						rows = append(rows, platform.IAMBindingPolicy{
							ID: "shared-policy", Scope: existingScope, MemberType: "USER", MemberValue: "user-id",
							RoleInfos: []platform.IAMRoleInfo{{ID: "role-id", RoleName: "afs.volumeOwner"}},
						})
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"policies": rows, "total_size": len(rows)}); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected request %s", r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","base_url":%q,"iam_base_url":%q}}}`, server.URL, server.URL)
			configPath := filepath.Join(t.TempDir(), "platform.json")
			if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
			t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
			client, err := platform.NewVirtualClusterClientFromEnvWithError()
			if err != nil {
				t.Fatal(err)
			}
			result, err := NewAuthService(client).GrantAFS(context.Background(), AuthGrantAFSRequest{
				ResourceType: "afs", ResourceName: "afs-target", Scope: "/rm" + scope,
				MemberType: "USER", MemberIdentifier: "tester", Role: "owner", DryRun: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			want := "dry-run"
			if existingScope != "" {
				want = "already exists"
			}
			if result.Result != want || policyCalls != 1 {
				t.Fatalf("result=%#v policyCalls=%d, want %q with one scoped lookup", result, policyCalls, want)
			}
		})
	}
}
