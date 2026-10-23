package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"rayctl/internal/platform"
)

func TestQueueAuthCommand(t *testing.T) {
	const queueName = "queue-d-elastic-a2-caicopilot"
	const ownerUID = "22222222-2222-4222-8222-222222222222"
	for _, tt := range []struct {
		name        string
		environment string
		identifiers []string
		long        bool
		wantError   bool
	}{
		{name: "name fragment and owning tenant", identifiers: []string{"elastic-a2-caicopilot"}, long: true},
		{name: "complete UID", identifiers: []string{ownerUID}},
		{name: "multiple resources", identifiers: []string{"queue-current", queueName}},
		{name: "PT isolation", environment: "pt", identifiers: []string{queueName}},
		{name: "successful resource survives batch failure", identifiers: []string{queueName, "does-not-exist"}, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			environment := tt.environment
			if environment == "" {
				environment = "d"
			}
			queues := map[string]platform.StorageVolumeResource{}
			for _, profile := range []string{"current", "owner", "current-pt"} {
				name, uid, region := queueName, ownerUID, "cn-pj-01"
				if profile == "current" {
					name, uid = "queue-current", "11111111-1111-4111-8111-111111111111"
				} else if profile == "current-pt" {
					uid, region = "33333333-3333-4333-8333-333333333333", "cn-pj-03"
				}
				queues[profile] = platform.StorageVolumeResource{ID: uid, Name: name, Type: "compute.ssp.v1.queue", Region: region, RID: fmt.Sprintf("/subscriptions/sub-%s/resourceGroups/default/regions/%s/clusters/cluster-test/queues/%s", profile, region, name)}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				profile := ""
				for name := range queues {
					if strings.Contains(r.Header.Get("Authorization"), `accesskey="`+name+`"`) {
						profile = name
					}
				}
				if (environment == "pt") != (profile == "current-pt") {
					t.Errorf("query crossed environment: %s %s", profile, r.URL)
				}
				queue := queues[profile]
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/rmh/v1/resources:page":
					if r.Method != http.MethodPost || !strings.Contains(r.URL.Query().Get("filter"), `resource_type="compute.ssp.v1.queue"`) {
						t.Errorf("unexpected metadata query: %s %s", r.Method, r.URL)
					}
					if tt.name == "complete UID" && strings.Contains(r.URL.Query().Get("filter"), "name=") {
						t.Errorf("UID was searched as a queue name: %s", r.URL)
					}
					json.NewEncoder(w).Encode(map[string]any{"resources": []platform.StorageVolumeResource{queue}})
				case "/iam/authz/v1/bindingPolicies":
					if r.Method != http.MethodGet || !strings.Contains(r.URL.Query().Get("filter"), `scope="*`+queue.Name+`*"`) {
						t.Errorf("policy query did not use the resolved owner's canonical name: %s %s", profile, r.URL)
					}
					scope := "/rm" + queue.RID
					policy := func(member, rid string) platform.IAMBindingPolicy {
						return platform.IAMBindingPolicy{ID: "shared-id", Scope: rid, MemberType: "GROUP", MemberName: member, MemberIdentify: member, MemberValue: member, RoleInfos: []platform.IAMRoleInfo{{RoleName: "rm.queue-role", DisplayName: "AI算力池队列查看"}}}
					}
					policies := []platform.IAMBindingPolicy{policy(profile+"-a", scope), policy(profile+"-b", scope), policy("parent", strings.Split(scope, "/queues/")[0]), policy("similar", scope+"-other"), policy("foreign", strings.Replace(scope, "sub-"+profile, "sub-foreign", 1))}
					json.NewEncoder(w).Encode(map[string]any{"policies": policies})
				default:
					t.Errorf("unexpected auth request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			profiles := map[string]any{}
			for name := range queues {
				region := "cn-pj-01"
				if name == "current-pt" {
					region = "cn-pj-03"
				}
				profiles[name] = map[string]any{"access_key": name, "secret_key": "test-secret", "subscription_id": "sub-" + name, "region": region, "base_url": server.URL, "iam_base_url": server.URL}
			}
			data, _ := json.Marshal(map[string]any{"current_profile": "current", "profiles": profiles})
			configPath := filepath.Join(t.TempDir(), "platform.json")
			if err := os.WriteFile(configPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
			t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", environment)
			root := &cobra.Command{Use: "rayctl", SilenceUsage: true, SilenceErrors: true}
			root.PersistentFlags().StringP("output", "o", "table", "")
			root.AddCommand(newAuthCmd())
			installJSONCommands(root)
			var out, errOut bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&errOut)
			args := append([]string{"auth", "check", "queue"}, tt.identifiers...)
			if tt.long {
				args = append(args, "-l")
			}
			root.SetArgs(append(args, "-o", "json"))
			err := root.Execute()
			if (err != nil) != tt.wantError {
				t.Fatalf("error=%v output=%s", err, out.String())
			}
			if len(tt.identifiers) == 1 {
				var public struct {
					ResourceType string `json:"resource_type"`
					ProfileName  string `json:"profile_name"`
					Scope        string `json:"scope"`
					Items        []struct {
						MemberName string `json:"member_name"`
					} `json:"items"`
				}
				if err := json.Unmarshal(out.Bytes(), &public); err != nil {
					t.Fatal(err)
				}
				owner := "owner"
				if environment == "pt" {
					owner = "current-pt"
				}
				if public.ResourceType != "QUEUE" || public.ProfileName != owner || public.Scope != "/rm"+queues[owner].RID || len(public.Items) != 2 || public.Items[0].MemberName != owner+"-a" || public.Items[1].MemberName != owner+"-b" {
					t.Fatalf("incorrect queue authorization: %s", out.String())
				}
			} else {
				var public struct {
					Items []struct {
						Context struct {
							ResourceName string `json:"resource_name"`
							ResourceType string `json:"resource_type"`
							Scope        string `json:"scope"`
						} `json:"context"`
						MemberName string `json:"member_name"`
					} `json:"items"`
					Metadata []json.RawMessage `json:"metadata"`
					Errors   []string          `json:"errors"`
				}
				if err := json.Unmarshal(out.Bytes(), &public); err != nil {
					t.Fatal(err, out.String())
				}
				want := len(tt.identifiers)
				if tt.wantError {
					want--
				}
				if len(public.Items) != want*2 || len(public.Metadata) != want || (len(public.Errors) > 0) != tt.wantError {
					t.Fatalf("partial queue results were lost: %s", out.String())
				}
				if want > 1 {
					for _, item := range public.Items {
						if item.Context.ResourceType != "QUEUE" || item.Context.ResourceName == "" || !strings.HasSuffix(item.Context.Scope, "/queues/"+item.Context.ResourceName) {
							t.Fatalf("batch binding lost its queue scope: %s", out.String())
						}
					}
				}
			}
		})
	}
}
