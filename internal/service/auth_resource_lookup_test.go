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
	"sync"
	"testing"

	"rayctl/internal/platform"
)

func vcAuthTestPolicy(scope, member string) platform.IAMBindingPolicy {
	return platform.IAMBindingPolicy{
		ID: "shared-policy", Scope: scope, MemberType: "GROUP", MemberIdentify: member, MemberValue: member,
		RoleInfos: []platform.IAMRoleInfo{{RoleName: "ecp.clusterUser", DisplayName: "VC User"}},
	}
}

func TestVCAuthUsesIAMScopeWhenDetailLookupCannotVerifyTenant(t *testing.T) {
	const currentScope = "/rm/subscriptions/sub-current/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-target"
	const otherScope = "/rm/subscriptions/sub-other/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-target"
	for _, tt := range []struct {
		name      string
		policies  map[string][]platform.IAMBindingPolicy
		verified  bool
		failIAM   bool
		profile   string
		scope     string
		count     int
		warning   bool
		wantError string
	}{
		{
			name: "current bindings survive wrong forbidden profile",
			policies: map[string][]platform.IAMBindingPolicy{
				"current": {vcAuthTestPolicy(currentScope, "group-a"), vcAuthTestPolicy(currentScope, "group-b")},
			},
			profile: "current", scope: currentScope, count: 2,
		},
		{
			name: "other tenant is confirmed by its exact IAM scope",
			policies: map[string][]platform.IAMBindingPolicy{
				"other": {vcAuthTestPolicy(otherScope, "group-other")},
			},
			profile: "other", scope: otherScope, count: 1,
		},
		{
			name: "no direct binding does not fabricate ownership",
			policies: map[string][]platform.IAMBindingPolicy{
				"current": {
					vcAuthTestPolicy(strings.TrimSuffix(currentScope, "/regions/cn-pj-01/virtualClusters/vc-target"), "parent-role"),
					vcAuthTestPolicy(currentScope+"-other", "similar-name"),
					vcAuthTestPolicy(currentScope+"/workloads/job", "child-resource"),
					vcAuthTestPolicy(strings.Replace(currentScope, "sub-current", "*", 1), "wildcard-tenant"),
					vcAuthTestPolicy(strings.Replace(currentScope, "cn-pj-01", "cn-pj-03", 1), "other-region"),
					vcAuthTestPolicy(strings.Replace(currentScope, "resourceGroups/default", "resourceGroups/other", 1), "other-group"),
				},
				"pt": {vcAuthTestPolicy(currentScope, "different-environment")},
			},
			warning: true,
		},
		{
			name: "ambiguous scopes fail rather than selecting first response",
			policies: map[string][]platform.IAMBindingPolicy{
				"other": {vcAuthTestPolicy(otherScope, "group-other")},
				"third": {vcAuthTestPolicy(strings.Replace(otherScope, "sub-other", "sub-third", 1), "group-third")},
			},
			wantError: "multiple tenants",
		},
		{
			name: "verified VC keeps its owning profile",
			policies: map[string][]platform.IAMBindingPolicy{
				"current": {vcAuthTestPolicy(currentScope, "current-group")},
				"other":   {vcAuthTestPolicy(otherScope, "unrelated-tenant")},
			},
			verified: true, profile: "current", scope: currentScope, count: 1,
		},
		{
			name:     "verified VC with no direct binding is an ordinary empty result",
			verified: true, profile: "current", scope: currentScope,
		},
		{
			name:    "IAM failure is not reported as no grants",
			failIAM: true, wantError: "denied",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			requests := map[string]int{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("auth check must only read: %s %s", r.Method, r.URL)
				}
				profile := ""
				for _, candidate := range []string{"current", "other", "third", "pt"} {
					if strings.Contains(r.Header.Get("Authorization"), `accesskey="`+candidate+`"`) {
						profile = candidate
						break
					}
				}
				mu.Lock()
				requests[profile+":"+r.URL.Path]++
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "/virtualClusters/") {
					if tt.verified && profile == "current" {
						fmt.Fprint(w, `{"uid":"vc-uid","name":"vc-target","tenant_id":"sub-current","region":"cn-pj-01"}`)
					} else if profile == "current" {
						http.Error(w, "VC detail unavailable", http.StatusNotFound)
					} else {
						http.Error(w, "VC detail forbidden", http.StatusForbidden)
					}
					return
				}
				if r.URL.Path != "/iam/authz/v1/bindingPolicies" {
					t.Errorf("unexpected request %s", r.URL)
					http.NotFound(w, r)
					return
				}
				if tt.failIAM && profile == "current" {
					http.Error(w, "IAM denied", http.StatusForbidden)
					return
				}
				if !strings.Contains(r.URL.Query().Get("filter"), "vc-target") {
					t.Errorf("unscoped binding lookup: %s", r.URL)
				}
				json.NewEncoder(w).Encode(map[string]any{"policies": tt.policies[profile]})
			}))
			defer server.Close()
			profiles := map[string]any{}
			for _, profile := range []string{"current", "other", "third", "pt"} {
				region := "cn-pj-01"
				if profile == "pt" {
					region = "cn-pj-03"
				}
				profiles[profile] = map[string]any{"access_key": profile, "secret_key": "test-secret", "subscription_id": "sub-" + profile, "region": region, "base_url": server.URL, "iam_base_url": server.URL}
			}
			config, _ := json.Marshal(map[string]any{"current_profile": "current", "profiles": profiles})
			configPath := filepath.Join(t.TempDir(), "platform.json")
			if err := os.WriteFile(configPath, config, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
			t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
			client, err := platform.NewVirtualClusterClientFromEnvWithError()
			if err != nil {
				t.Fatal(err)
			}
			result, err := NewAuthService(client).GetResourceAuth(context.Background(), "vc", "vc-target")
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error=%v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.ProfileName != tt.profile || result.Scope != tt.scope || len(result.Items) != tt.count || (len(result.Warnings) > 0) != tt.warning {
				t.Fatalf("incorrect resolved auth: %#v", result)
			}
			mu.Lock()
			defer mu.Unlock()
			for key := range requests {
				if strings.HasPrefix(key, "pt:") {
					t.Fatalf("unexpected cross-environment request: %s", key)
				}
			}
			if tt.verified && requests["other:/iam/authz/v1/bindingPolicies"] != 0 {
				t.Fatal("verified VC queried another tenant's bindings")
			}
		})
	}
}
