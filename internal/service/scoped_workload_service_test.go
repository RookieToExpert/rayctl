package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"rayctl/internal/platform"
)

func workloadTestPlatform(t *testing.T, handler http.HandlerFunc) *platform.VirtualClusterClient {
	return workloadTestPlatformForEnvironment(t, handler, "")
}

func workloadTestPlatformForEnvironment(t *testing.T, handler http.HandlerFunc, environment string) *platform.VirtualClusterClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	configPath := filepath.Join(t.TempDir(), "platform.json")
	iamURL := server.URL
	if environment == platform.ClusterDCloud {
		iamURL = "https://iam-cloud.d.pjlab.org.cn"
	}
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","region":"cn-pj-01","resource_group":"default","base_url":%q,"kubernetes_base_url":%q,"iam_base_url":%q}}}`, server.URL, server.URL, iamURL)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestWorkspaceWorkloadsAggregateSharedQueueAndPartialFailure(t *testing.T) {
	client := workloadTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rmh/v1/resources:page":
			fmt.Fprint(w, `{"resources":[{"id":"workspace-uid","name":"ws-demo","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/ws-demo"}]}`)
		case strings.HasSuffix(r.URL.Path, "/workspaces/ws-demo/queues"):
			fmt.Fprint(w, `{"queues":[{"name":"queue-good","uid":"q1","properties":{"cluster_name":"ssp-demo"}},{"name":"queue-fail","uid":"q2","properties":{"cluster_name":"ssp-demo"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/queue-good/workloads"):
			if got := r.URL.Query().Get("filter"); got != `type="trainingJob" AND state="Running" AND priority="NORMAL"` {
				t.Errorf("filter = %q", got)
			}
			fmt.Fprint(w, `{"workloads":[{"name":"foreign","uid":"f","type":"trainingJob","workspace":{"name":"ws-other","uid":"other-uid"},"create_time":"2026-10-08T08:00:00Z"},{"name":"older","uid":"o","type":"trainingJob","workspace":{"name":"ws-demo"},"create_time":"2026-10-08T06:00:00Z"},{"name":"newer","uid":"n","type":"trainingJob","workspace":{"uid":"workspace-uid"},"create_time":"2026-10-08T07:00:00Z"},{"name":"newer","uid":"n","type":"trainingJob","workspace":{"uid":"workspace-uid"},"create_time":"2026-10-08T07:00:00Z"}]}`)
		case strings.HasSuffix(r.URL.Path, "/queue-fail/workloads"):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	result, err := NewSSPResourceService(nil, client).ListWorkspaceWorkloads(context.Background(), "workspace-uid", "cn-pj-01", SSPQueueWorkloadQuery{Type: "trainingJob", State: "Running", Priority: "NORMAL", Limit: 1})
	if err == nil || result == nil || len(result.Warnings) != 1 || len(result.Items) != 1 || result.Items[0].Name != "newer" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if result.Items[0].Workspace != "ws-demo" || result.Items[0].Queue != "queue-good" {
		t.Fatalf("workload location = %#v", result.Items[0])
	}
}

func TestWorkspaceWorkloadsEmptyAndAllFailed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%v", fail), func(t *testing.T) {
			client := workloadTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/rmh/v1/resources:page":
					fmt.Fprint(w, `{"resources":[{"id":"uid","name":"ws","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/ws"}]}`)
				case strings.HasSuffix(r.URL.Path, "/workspaces/ws/queues"):
					if fail {
						fmt.Fprint(w, `{"queues":[{"name":"q","properties":{"cluster_name":"ssp"}}]}`)
					} else {
						fmt.Fprint(w, `{"queues":[]}`)
					}
				case strings.HasSuffix(r.URL.Path, "/q/workloads"):
					http.Error(w, "forbidden", http.StatusForbidden)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			result, err := NewSSPResourceService(nil, client).ListWorkspaceWorkloads(context.Background(), "ws", "", SSPQueueWorkloadQuery{})
			if result == nil || len(result.Items) != 0 || (err != nil) != fail || (len(result.Warnings) != 0) != fail {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}

func TestWorkspaceWorkloadsReadsAllPagesBeforeLimit(t *testing.T) {
	var queuePages, workloadPages atomic.Int32
	client := workloadTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rmh/v1/resources:page":
			fmt.Fprint(w, `{"resources":[{"id":"uid","name":"ws","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/ws"}]}`)
		case strings.HasSuffix(r.URL.Path, "/workspaces/ws/queues"):
			queuePages.Add(1)
			if r.URL.Query().Get("skip") == "0" {
				fmt.Fprint(w, `{"queues":[`)
				for index := range 100 {
					if index > 0 {
						fmt.Fprint(w, ",")
					}
					fmt.Fprintf(w, `{"name":"q-%d","properties":{"cluster_name":"ssp"}}`, index)
				}
				fmt.Fprint(w, `],"total_size":101}`)
			} else {
				fmt.Fprint(w, `{"queues":[{"name":"q-last","properties":{"cluster_name":"ssp"}}],"total_size":101}`)
			}
		case strings.HasSuffix(r.URL.Path, "/q-last/workloads"):
			workloadPages.Add(1)
			if r.URL.Query().Get("skip") == "0" {
				fmt.Fprint(w, `{"workloads":[`)
				for index := range 100 {
					if index > 0 {
						fmt.Fprint(w, ",")
					}
					fmt.Fprintf(w, `{"name":"foreign-%d","workspace":{"name":"other"}}`, index)
				}
				fmt.Fprint(w, `],"total_size":101}`)
			} else {
				fmt.Fprint(w, `{"workloads":[{"name":"target","uid":"target-uid","type":"aid","workspace":{"name":"ws"},"create_time":"2026-10-08T07:00:00Z"}],"total_size":101}`)
			}
		case strings.HasSuffix(r.URL.Path, "/workloads"):
			fmt.Fprint(w, `{"workloads":[]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	result, err := NewSSPResourceService(nil, client).ListWorkspaceWorkloads(context.Background(), "ws", "", SSPQueueWorkloadQuery{Limit: 1})
	if err != nil || result == nil || len(result.Items) != 1 || result.Items[0].Name != "target" || queuePages.Load() != 2 || workloadPages.Load() != 2 {
		t.Fatalf("result=%#v error=%v pages=%d/%d", result, err, queuePages.Load(), workloadPages.Load())
	}
}

func TestScopedWorkloadsDeduplicateWithinWorkspaceAndNamespace(t *testing.T) {
	items := []SSPQueueWorkloadItem{{Type: "vcJob", Name: "same", Namespace: "a"}, {Type: "vcJob", Name: "same", Namespace: "b"}, {Type: "aid", Name: "same", Workspace: "a"}, {Type: "aid", Name: "same", Workspace: "b"}, {Type: "aid", UID: "uid", Name: "uid-first"}, {Type: "aid", UID: "uid", Name: "uid-again"}}
	if got := sortScopedWorkloads(items, 0); len(got) != 5 {
		t.Fatalf("deduplicated items = %#v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := (&SSPResourceService{}).collectScopedQueueWorkloads(ctx, nil, nil, SSPQueueWorkloadQuery{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("empty canceled workload scope error = %v", err)
	}
}
