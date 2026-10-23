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

func TestNodeWorkloadsCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/nodes/worker":
			fmt.Fprint(w, `{"apiVersion":"v1","kind":"Node","metadata":{"name":"worker"}}`)
		case "/api/v1/pods":
			if r.URL.Query().Get("fieldSelector") != "spec.nodeName=worker" {
				t.Errorf("missing node selector: %s", r.URL)
			}
			fmt.Fprint(w, `{"apiVersion":"v1","kind":"PodList","items":[{"metadata":{"name":"pod","namespace":"ns","labels":{"resource.compute.sensecore.cn/workload-type":"aid","resource.compute.sensecore.cn/workload-name":"real-aid"}},"spec":{"nodeName":"worker"},"status":{"phase":"Running"}}]}`)
		case "/api/v1/nodes/missing":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	config := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: local\n  cluster:\n    server: %s\ncontexts:\n- name: local\n  context:\n    cluster: local\n    user: local\ncurrent-context: local\nusers:\n- name: local\n  user: {}\n", server.URL)
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	badConfig := filepath.Join(dir, "platform.json")
	if err := os.WriteFile(badConfig, []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", badConfig)
	old := kubeconfig
	kubeconfig = path
	t.Cleanup(func() { kubeconfig = old })
	for _, tc := range []struct {
		name                  string
		noPlatform, wantError bool
	}{{"worker", true, false}, {"worker", false, false}, {"missing", true, true}} {
		command := newNodeGetCmd()
		command.Flags().String("output", "table", "")
		installJSONCommands(command)
		var buf bytes.Buffer
		command.SetOut(&buf)
		args := []string{tc.name, "--workloads", "--output", "json"}
		if tc.noPlatform {
			args = append(args, "--no-platform")
		}
		command.SetArgs(args)
		err := command.Execute()
		if (err != nil) != tc.wantError {
			t.Fatalf("%+v: %v", tc, err)
		}
		if !json.Valid(buf.Bytes()) {
			t.Fatal(buf.String())
		}
		if !tc.wantError && (!strings.Contains(buf.String(), "real-aid") || !strings.Contains(buf.String(), `"source": "k8s"`)) {
			t.Fatal(buf.String())
		}
		if !tc.wantError && !tc.noPlatform && !strings.Contains(buf.String(), "platform unavailable") {
			t.Fatal("missing degradation warning")
		}
		if tc.noPlatform && strings.Contains(buf.String(), "platform unavailable") {
			t.Fatal("--no-platform read platform config")
		}
	}
}
