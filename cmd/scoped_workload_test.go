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
)

func configureScopedWorkloadTest(t *testing.T, serverURL string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "platform.json")
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","region":"cn-pj-03","base_url":%q,"kubernetes_base_url":%q}}}`, serverURL, serverURL)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	oldKubeconfig, oldEnvironment := kubeconfig, targetEnvironment
	kubeconfig, targetEnvironment = filepath.Join(t.TempDir(), "missing-kubeconfig"), "pt"
	t.Cleanup(func() { kubeconfig, targetEnvironment = oldKubeconfig, oldEnvironment })
}

func TestWorkspaceWorkloadCommandKeepsPartialResultsInJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rmh/v1/resources:page":
			fmt.Fprint(w, `{"resources":[{"id":"uid-a","name":"ws-a","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-03/workspaces/ws-a"},{"id":"uid-b","name":"ws-b","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-03/workspaces/ws-b"}]}`)
		case strings.HasSuffix(r.URL.Path, "/workspaces/ws-a/queues"):
			fmt.Fprint(w, `{"queues":[{"name":"q-a","properties":{"cluster_name":"cluster-test"}},{"name":"q-broken","properties":{"cluster_name":"cluster-test"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/workspaces/ws-b/queues"):
			fmt.Fprint(w, `{"queues":[{"name":"q-b","properties":{"cluster_name":"cluster-test"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/queues/q-broken/workloads"):
			http.Error(w, "queue access denied", http.StatusForbidden)
		case strings.HasSuffix(r.URL.Path, "/workloads"):
			if filter := r.URL.Query().Get("filter"); filter != `type="trainingJob" AND state="Running" AND priority="NORMAL"` {
				t.Errorf("filter = %q", filter)
			}
			name := "a"
			if strings.HasSuffix(r.URL.Path, "/queues/q-b/workloads") {
				name = "b"
			}
			fmt.Fprintf(w, `{"workloads":[{"name":"job-%s","uid":"job-uid-%s","type":"trainingJob","state":"Running","priority":"NORMAL","workspace":{"name":"ws-%s"}}]}`, name, name, name)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureScopedWorkloadTest(t, server.URL)
	root := &cobra.Command{Use: "rayctl", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringP("output", "o", "table", "")
	root.AddCommand(newWorkspaceCmd())
	installJSONCommands(root)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"ws", "wl", "list", "ws-b", "uid-a", "-t", "job", "--state", "Running", "--priority", "NORMAL", "-o", "json"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "q-broken") {
		t.Fatalf("partial failure error = %v", err)
	}
	var payload struct {
		Items []struct {
			Name      string `json:"name"`
			Workspace string `json:"workspace"`
			Queue     string `json:"queue"`
		} `json:"items"`
		Summary struct {
			Complete  bool `json:"complete"`
			Requested int  `json:"requested"`
		} `json:"summary"`
		Warnings []string `json:"warnings"`
		Errors   []string `json:"errors"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err, out.String())
	}
	if len(payload.Items) != 2 || payload.Items[0].Name != "job-b" || payload.Items[1].Name != "job-a" || payload.Items[1].Queue != "q-a" || payload.Items[1].Workspace != "ws-a" {
		t.Fatalf("successful items or input order lost: %s", out.String())
	}
	if payload.Summary.Complete || payload.Summary.Requested != 2 || len(payload.Errors) == 0 || len(payload.Warnings) == 0 || !strings.Contains(errOut.String(), "q-broken") {
		t.Fatalf("partial failure not reported: stdout=%s stderr=%s", out.String(), errOut.String())
	}
}

func TestVCWorkloadCommandECPFiltersAndContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/compute/ecp/") && strings.Contains(r.URL.Path, "/virtualClusters/vc-"):
			name := strings.TrimPrefix(filepath.Base(r.URL.Path), "vc-")
			fmt.Fprintf(w, `{"name":"vc-%s","uid":"uid-%s","tenant_id":"sub","region":"cn-pj-03"}`, name, name)
		case strings.HasSuffix(r.URL.Path, "/apis/batch.volcano.sh/v1alpha1/jobs"):
			if r.URL.Query().Get("filter") != `state="Running"` {
				t.Errorf("ECP state filter = %q", r.URL.Query().Get("filter"))
			}
			name := "a"
			if strings.Contains(r.URL.Path, "/virtualClusters/vc-uid-b/") {
				name = "b"
			}
			fmt.Fprintf(w, `{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"JobList","items":[{"metadata":{"name":"job-%s","namespace":"default","uid":"job-uid-%s"},"spec":{"queue":"ecp-queue","priorityClassName":"NORMAL"},"status":{"state":{"phase":"Running"}}},{"metadata":{"name":"filtered"},"spec":{"priorityClassName":"HIGH"},"status":{"state":{"phase":"Running"}}}]}`, name, name)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureScopedWorkloadTest(t, server.URL)
	root := &cobra.Command{Use: "rayctl", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringP("output", "o", "table", "")
	root.AddCommand(newVCCmd())
	installJSONCommands(root)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"vc", "wl", "list", "vc-b", "vc-a", "-t", "vcJob", "--state", "Running", "--priority", "NORMAL", "-o", "json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	var payload struct {
		Items []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Context struct {
				ClusterName string `json:"cluster_name"`
			} `json:"context"`
		} `json:"items"`
		Summary struct {
			Complete bool `json:"complete"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err, out.String())
	}
	if len(payload.Items) != 2 || payload.Items[0].Name != "job-b" || payload.Items[1].Name != "job-a" || payload.Items[0].Type != "vcJob" || payload.Items[0].Context.ClusterName != "vc-b" || !payload.Summary.Complete {
		t.Fatalf("ECP filtering, ordering, or context failed: %s", out.String())
	}
}
