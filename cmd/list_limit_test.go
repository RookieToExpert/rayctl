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
	"time"

	"github.com/spf13/cobra"
	"rayctl/pkg/output"
)

func TestListWindowPreservesExplicitLimit(t *testing.T) {
	// Return 53 recent records even for a smaller requested page, so the
	// assertions also verify the final output limit.
	now := time.Now().UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rmh/v1/resources:page":
			if strings.Contains(r.URL.Query().Get("filter"), "compute.ssp.v1.queue") {
				fmt.Fprint(w, `{"resources":[{"name":"queue-test","type":"compute.ssp.v1.queue","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-03/clusters/cluster-test/queues/queue-test"}]}`)
				return
			}
			fmt.Fprint(w, `{"resources":[{"name":"ws-test","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-03/workspaces/ws-test"}]}`)
		case strings.HasSuffix(r.URL.Path, "/virtualClusters"):
			fmt.Fprint(w, `{"virtual_clusters":[{"uid":"test","name":"vc-test"}]}`)
		case strings.HasSuffix(r.URL.Path, "/trainingJobs"), strings.HasSuffix(r.URL.Path, "/aids"), strings.HasSuffix(r.URL.Path, "/jobs"), strings.HasSuffix(r.URL.Path, "/workloads"):
			key := "training_jobs"
			if strings.HasSuffix(r.URL.Path, "/aids") {
				key = "aids"
			} else if strings.HasSuffix(r.URL.Path, "/jobs") {
				key = "items"
			} else if strings.HasSuffix(r.URL.Path, "/workloads") {
				key = "workloads"
			}
			items := make([]map[string]any, 53)
			for i := range items {
				name := fmt.Sprintf("job-%02d", i)
				items[i] = map[string]any{
					"name": name, "state": "Running", "create_time": now,
					"metadata": map[string]any{"name": name, "namespace": "default", "creationTimestamp": now},
					"status":   map[string]any{"state": "Running", "create_time": now},
				}
				if key == "items" {
					items[i]["apiVersion"], items[i]["kind"] = "batch.volcano.sh/v1alpha1", "Job"
					items[i]["status"] = map[string]any{"state": map[string]any{"phase": "Running"}}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{key: items, "total_size": 53, "apiVersion": "batch.volcano.sh/v1alpha1", "kind": "JobList"})
		default:
			t.Errorf("unexpected platform request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "platform.json")
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","region":"cn-pj-03","base_url":%q,"kubernetes_base_url":%q}}}`, server.URL, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	oldKubeconfig, oldEnvironment := kubeconfig, targetEnvironment
	kubeconfig, targetEnvironment = filepath.Join(t.TempDir(), "missing"), "pt"
	t.Cleanup(func() { kubeconfig, targetEnvironment = oldKubeconfig, oldEnvironment })

	for _, command := range []struct {
		name string
		new  func() *cobra.Command
		args []string
	}{{"ait", newAITListCmd, nil}, {"aid", newAIDListCmd, nil}, {"ate", newECPJobListCmd, nil}, {"ait queue", newAITListCmd, []string{"-q", "queue-test"}}, {"aid queue", newAIDListCmd, []string{"-q", "queue-test"}}} {
		for _, test := range []struct {
			name string
			args []string
			want int
		}{
			{"default", nil, 50},
			{"since explicit", []string{"--since", "7d", "-n", "3"}, 3},
			{"all explicit", []string{"-A", "-n", "3"}, 3},
			{"since explicit default", []string{"--since", "7d", "--limit", "50"}, 50},
			{"since default", []string{"--since", "7d"}, 53},
			{"all default", []string{"-A"}, 53},
		} {
			t.Run(command.name+"/"+test.name, func(t *testing.T) {
				cmd := command.new()
				cmd.SetContext(t.Context())
				args := append(append([]string{}, command.args...), test.args...)
				if err := cmd.ParseFlags(args); err != nil {
					t.Fatal(err)
				}
				session := output.BeginJSON()
				defer output.EndJSON()
				if err := cmd.RunE(cmd, nil); err != nil {
					t.Fatal(err)
				}
				var buffer bytes.Buffer
				if err := session.Write(&buffer, true, 0, nil); err != nil {
					t.Fatal(err)
				}
				var result struct {
					Items []json.RawMessage `json:"items"`
				}
				if err := json.Unmarshal(buffer.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Items) != test.want {
					t.Fatalf("returned %d records, want %d", len(result.Items), test.want)
				}
			})
		}
	}
}
