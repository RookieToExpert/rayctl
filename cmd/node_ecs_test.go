package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNodeGetECSBusinessNameWithoutPlatform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/nodes/host-10-12-104-6":
			fmt.Fprint(w, `{"apiVersion":"v1","kind":"Node","metadata":{"name":"host-10-12-104-6","labels":{"node-role.compute.sensecore.cn/prod":"ecs"}}}`)
		case "/api/v1/pods":
			if r.URL.Query().Get("fieldSelector") != "spec.nodeName=host-10-12-104-6" {
				t.Errorf("missing node selector: %s", r.URL)
			}
			fmt.Fprint(w, `{"apiVersion":"v1","kind":"PodList","items":[{"metadata":{"name":"virt-launcher-ecs-b397575f3132f5a8-z9cvj","namespace":"ecs-user","annotations":{"resource.compute.sensecore.cn/name":"ecs-yunwei","resource.compute.sensecore.cn/uid":"019fc69a-98d5-70b4-9745-957244ee092f"},"ownerReferences":[{"apiVersion":"kubevirt.io/v1","kind":"VirtualMachineInstance","name":"ecs-b397575f3132f5a8","uid":"0e08f1fa-501f-45c4-9c94-3b8734e034bb","controller":true}]},"spec":{"nodeName":"host-10-12-104-6"},"status":{"phase":"Running"}}]}`)
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
	old := kubeconfig
	kubeconfig = path
	t.Cleanup(func() { kubeconfig = old })
	t.Setenv("RAYCTL_PLATFORM_CONFIG", filepath.Join(dir, "missing-platform.json"))
	for _, workloads := range []bool{false, true} {
		t.Run(fmt.Sprintf("workloads=%t", workloads), func(t *testing.T) {
			command := newNodeGetCmd()
			command.Flags().String("output", "table", "")
			installJSONCommands(command)
			var buf bytes.Buffer
			command.SetOut(&buf)
			args := []string{"10.12.104.6", "--no-platform", "--output", "json"}
			if workloads {
				// Workload mode resolves IPs using Node addresses; the mock uses its name.
				args[0] = "host-10-12-104-6"
				args = append(args, "--workloads")
			}
			command.SetArgs(args)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			var result struct {
				PodDetails []struct {
					Pod, Type, Workload string
					WorkloadUID         string `json:"workload_uid"`
				} `json:"pod_details"`
				Workloads []struct {
					Pod, Type, Workload string
					WorkloadUID         string `json:"workload_uid"`
				}
			}
			if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
				t.Fatalf("decode result: %v: %s", err, buf.String())
			}
			if len(result.PodDetails) != 1 || result.PodDetails[0].Workload != "ecs-yunwei" || result.PodDetails[0].Type != "ECS" || result.PodDetails[0].WorkloadUID != "019fc69a-98d5-70b4-9745-957244ee092f" {
				t.Fatalf("incorrect ECS Pod business identity: %s", buf.String())
			}
			if workloads && (len(result.Workloads) != 1 || result.Workloads[0].Type != "ECS" || result.Workloads[0].Workload != "ecs-yunwei") {
				t.Fatalf("incorrect ECS workload identity: %s", buf.String())
			}
		})
	}
}
