package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

func TestECSListJSONInventoryWithoutKubeconfig(t *testing.T) {
	for _, tt := range []struct {
		name       string
		args       []string
		aisFailure bool
		returned   int
		total      int
	}{
		{name: "default cap", returned: 100, total: 131},
		{name: "filter on later page", args: []string{"-t", "ecs", "--name", "PRODUCTION", "-s", "running", "-n", "1"}, returned: 1, total: 1},
		{name: "all", args: []string{"-n", "0"}, returned: 131, total: 131},
		{name: "partial source", aisFailure: true, returned: 100, total: 130},
		{name: "empty", args: []string{"--name", "does-not-exist"}, returned: 0, total: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var ecsRequests, aisRequests, iamRequests, otherRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/compute/ecs/v2/subscriptions/-/resourceGroups/-/zones/-/virtualMachines":
					ecsRequests.Add(1)
					skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
					items := make([]map[string]any, 0)
					for index := skip; index < min(skip+100, 130); index++ {
						state, displayName := "STOPPED", ""
						if index == 120 {
							state, displayName = "RUNNING", "Production"
						}
						items = append(items, map[string]any{
							"name": fmt.Sprintf("ecs-%03d", index), "uid": fmt.Sprintf("uid-%03d", index),
							"display_name": displayName, "state": state, "creator_id": "creator-a",
						})
					}
					json.NewEncoder(w).Encode(map[string]any{"virtual_machines": items})
				case "/compute/ais/v1/subscriptions/-/resourceGroups/-/zones/-/aiSpaces":
					aisRequests.Add(1)
					if tt.aisFailure {
						http.Error(w, "AIS access denied", http.StatusForbidden)
						return
					}
					fmt.Fprint(w, `{"ai_spaces":[{"name":"ais-a","uid":"ais-uid","creator_id":"creator-a","state":"RUNNING","properties":{"host_ip":"10.0.0.2"}}]}`)
				case "/iam/idp/v1/getUsers":
					iamRequests.Add(1)
					if r.URL.Query().Get("filter") != `id="creator-a"` {
						t.Errorf("creator filter = %q", r.URL.Query().Get("filter"))
					}
					fmt.Fprint(w, `{"users":[{"id":"creator-a","username":"alice"}]}`)
				default:
					t.Errorf("unexpected inventory request: %s", r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				otherRequests.Add(1)
				http.Error(w, "wrong profile", http.StatusForbidden)
			}))
			defer other.Close()
			configPath := filepath.Join(t.TempDir(), "platform.json")
			config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","base_url":%q,"iam_base_url":%q},"other":{"access_key":"other-ak","secret_key":"other-sk","subscription_id":"other-sub","base_url":%q,"iam_base_url":%q}}}`, server.URL, server.URL, other.URL, other.URL)
			if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
			t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
			previousKubeconfig := kubeconfig
			kubeconfig = filepath.Join(t.TempDir(), "missing-kubeconfig")
			t.Cleanup(func() { kubeconfig = previousKubeconfig })
			root := &cobra.Command{Use: "rayctl", SilenceUsage: true, SilenceErrors: true}
			root.PersistentFlags().StringP("output", "o", "table", "")
			root.AddCommand(newECSCmd())
			installJSONCommands(root)
			var out, errOut bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&errOut)
			root.SetArgs(append([]string{"ecs", "list", "-o", "json"}, tt.args...))
			err := root.Execute()
			if (err != nil) != tt.aisFailure {
				t.Fatalf("command error=%v stdout=%s stderr=%s", err, out.String(), errOut.String())
			}
			var payload struct {
				Items []struct {
					Name, Creator string
				} `json:"items"`
				Summary struct {
					Returned int
					Total    int
					Complete bool
				} `json:"summary"`
				Metadata []struct{ Truncated bool } `json:"metadata"`
				Warnings []string                   `json:"warnings"`
				Errors   []string                   `json:"errors"`
			}
			if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
				t.Fatalf("invalid JSON: %v\n%s", err, out.String())
			}
			if len(payload.Items) != tt.returned || payload.Summary.Returned != tt.returned || payload.Summary.Total != tt.total || payload.Summary.Complete == tt.aisFailure {
				t.Fatalf("unexpected inventory summary: %s", out.String())
			}
			if len(payload.Metadata) != 1 || payload.Metadata[0].Truncated != (tt.total > tt.returned) {
				t.Fatalf("incorrect truncation metadata: %s", out.String())
			}
			for _, item := range payload.Items {
				if item.Creator != "alice" {
					t.Fatalf("creator = %q, want alice", item.Creator)
				}
			}
			if tt.name == "filter on later page" && (payload.Items[0].Name != "ecs-120" || aisRequests.Load() != 0) {
				t.Fatalf("filter/typed inventory mismatch: stdout=%s AIS calls=%d", out.String(), aisRequests.Load())
			}
			if tt.aisFailure && (len(payload.Errors) == 0 || len(payload.Warnings) == 0 || !strings.Contains(errOut.String(), "AIS")) {
				t.Fatalf("partial inventory failure not visible: stdout=%s stderr=%s", out.String(), errOut.String())
			}
			wantIAMCalls := int32(1)
			if tt.returned == 0 {
				wantIAMCalls = 0
			}
			if ecsRequests.Load() != 2 || iamRequests.Load() != wantIAMCalls || otherRequests.Load() != 0 {
				t.Fatalf("unexpected request counts: ECS=%d IAM=%d other-profile=%d", ecsRequests.Load(), iamRequests.Load(), otherRequests.Load())
			}
		})
	}
}

func TestECSListValidatesBeforeLoadingPlatformConfiguration(t *testing.T) {
	t.Setenv("RAYCTL_PLATFORM_CONFIG", filepath.Join(t.TempDir(), "missing-platform.json"))
	for _, args := range [][]string{{"--type", "pod"}, {"-n", "-1"}} {
		cmd := newECSListCmd()
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatal(err)
		}
		if err := cmd.RunE(cmd, nil); err == nil || !strings.Contains(err.Error(), "--") {
			t.Fatalf("expected validation error before config lookup: %v", err)
		}
	}
}
