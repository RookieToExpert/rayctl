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
)

func TestPodCommandDeletingNoPlatformJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/host/pods/pod" {
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"pod","namespace":"host","deletionTimestamp":"2026-10-14T00:00:00Z","labels":{"resource.compute.sensecore.cn/workload-type":"aid","resource.compute.sensecore.cn/workload-name":"real-aid"}},"status":{"phase":"Running","podIP":"10.0.0.1","conditions":[{"type":"Ready","status":"True"}]}}`)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	config := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: local\n  cluster:\n    server: %s\ncontexts:\n- name: local\n  context:\n    cluster: local\n    user: local\ncurrent-context: local\nusers:\n- name: local\n  user: {}\n", server.URL)
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	old := kubeconfig
	kubeconfig = path
	t.Cleanup(func() { kubeconfig = old })
	t.Setenv("HOME", t.TempDir())
	parent := newPodCmd()
	parent.PersistentFlags().String("output", "table", "")
	installJSONCommands(parent)
	var buf bytes.Buffer
	parent.SetOut(&buf)
	parent.SetArgs([]string{"get", "host/pod", "--no-platform", "--output", "json"})
	if err := parent.Execute(); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(buf.Bytes()) || !strings.Contains(buf.String(), "real-aid") || !strings.Contains(buf.String(), `"host_pod": "pod"`) {
		t.Fatal(buf.String())
	}
	var result struct {
		Status      string `json:"status"`
		Phase       string `json:"phase"`
		Ready       bool   `json:"ready"`
		DeletingFor string `json:"deleting_for"`
	}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "Terminating" || result.Phase != "Running" || !result.Ready || result.DeletingFor == "" {
		t.Fatalf("deleting pod result: %+v", result)
	}
}
