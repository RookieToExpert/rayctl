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

	"rayctl/pkg/output"
)

func TestQueueUsageHelpExplainsQuotaAndFreeScope(t *testing.T) {
	cmd := newQueueNodeUsageCmd()
	for _, text := range []string{"平台资源页", "默认只显示本队列已占用", "vc node usage <vc>", "--free", "不校验队列配额", "配额表不受筛选影响", "公共池未归属", "归属未知", "REMAINING"} {
		if !strings.Contains(cmd.Long, text) {
			t.Fatalf("help missing %q", text)
		}
	}
	if !strings.Contains(cmd.Flags().Lookup("free").Usage, "不校验队列配额") {
		t.Fatal(cmd.Flags().Lookup("free").Usage)
	}
}

func TestQueueUsageCommandPreservesPartialDataAndJSON(t *testing.T) {
	for _, nodeFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(nodeFailure), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/rmh/v1/resources:page":
					fmt.Fprint(w, `{"resources":[{"id":"queue-uid","name":"queue-test","type":"compute.ssp.v1.queue","state":"RUNNING","region":"cn-pj-01","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/cluster/queues/queue-test","properties":"{\"type\":\"EXCLUSIVE\"}"}],"total_size":1}`)
				case strings.HasSuffix(r.URL.Path, "/queues/queue-test/nodes"):
					if nodeFailure {
						w.WriteHeader(403)
						return
					}
					fmt.Fprint(w, `{"nodes":[{"name":"host-a","host_ip":"10.0.0.1","summary_data":[{"resource_type":"CPU","total":"16","allocated":"8","unallocated":"8","unit":"core"},{"resource_type":"MEMORY","total":"32","allocated":"16","unallocated":"16","unit":"GiB"}]}],"total_size":1}`)
				case strings.HasSuffix(r.URL.Path, "/queues/queue-test"):
					if !nodeFailure {
						w.WriteHeader(403)
						return
					}
					fmt.Fprint(w, `{"properties":{"quota":[{"resource_type":"CPU","unit":"core","total":"4","allocated":"4","unallocated":"0"}]}}`)
				case r.URL.Path == "/api/v1/namespaces":
					fmt.Fprint(w, `{"apiVersion":"v1","kind":"NamespaceList","items":[]}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			configPath := filepath.Join(dir, "platform.json")
			kubePath := filepath.Join(dir, "kubeconfig")
			config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","region":"cn-pj-01","base_url":%q,"kubernetes_base_url":%q}}}`, server.URL, server.URL)
			if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			config = fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: local\n  cluster:\n    server: %s\ncontexts:\n- name: local\n  context:\n    cluster: local\n    user: local\ncurrent-context: local\nusers:\n- name: local\n  user: {}\n", server.URL)
			if err := os.WriteFile(kubePath, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
			t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
			oldKubeconfig, oldEnvironment := kubeconfig, targetEnvironment
			kubeconfig, targetEnvironment = kubePath, "d"
			t.Cleanup(func() { kubeconfig, targetEnvironment = oldKubeconfig, oldEnvironment })
			cmd := newQueueNodeUsageCmd()
			cmd.SetContext(t.Context())
			if err := cmd.ParseFlags([]string{"--free"}); err != nil {
				t.Fatal(err)
			}
			session := output.BeginJSON()
			defer output.EndJSON()
			err := cmd.RunE(cmd, []string{"queue-test"})
			if err == nil || !strings.Contains(err.Error(), "HTTP 403") || strings.Contains(err.Error(), "http://") {
				t.Fatal(err)
			}
			var buffer bytes.Buffer
			if err := session.Write(&buffer, true, 1, err); err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Items    []map[string]any
				Metadata []map[string]any
				Summary  struct{ Complete bool }
			}
			if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Metadata) != 1 || payload.Summary.Complete {
				t.Fatal(buffer.String())
			}
			if nodeFailure {
				if payload.Metadata[0]["quota"] == nil || len(payload.Items) != 0 {
					t.Fatal(buffer.String())
				}
			} else if len(payload.Items) != 1 || payload.Metadata[0]["quota_error"] == nil {
				t.Fatal(buffer.String())
			}
		})
	}
}
