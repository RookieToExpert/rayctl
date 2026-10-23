package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rayctl/internal/platform"
)

func newSSPQueryTestClient(t *testing.T, handler http.HandlerFunc) *platform.VirtualClusterClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "platform.json")
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","region":"cn-pj-01","base_url":%q,"kubernetes_base_url":%q}}}`, server.URL, server.URL)
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeTestWorkspaces(w http.ResponseWriter, names ...string) {
	resources := []map[string]string{}
	for _, name := range names {
		resources = append(resources, map[string]string{"id": "uid-" + name, "name": name, "type": "compute.ssp.v1.workspace", "region": "cn-pj-01", "rid": "/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/" + name})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"resources": resources, "total_size": len(resources)})
}

func TestAIRListKeepsAllWorkspaceFailuresAlongsideData(t *testing.T) {
	for _, test := range []struct {
		name                          string
		empty, allForbidden, filtered bool
	}{
		{name: "has rows"}, {name: "successful empty", empty: true}, {name: "filter removes rows", filtered: true}, {name: "all forbidden", allForbidden: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/rmh/v1/resources:page" {
					writeTestWorkspaces(w, "readable", "skip-webhook-test", "skip-hostpath-webhook")
					return
				}
				if strings.Contains(r.URL.Path, "/workspaces/readable/airs") && !test.allForbidden {
					jobs := []map[string]any{}
					if !test.empty {
						jobs = append(jobs, map[string]any{"name": "infer-found", "uid": "job-uid", "status": map[string]any{"state": "RUNNING"}})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"airs": jobs, "total_size": len(jobs)})
					return
				}
				w.WriteHeader(403)
				fmt.Fprint(w, `{"message":"PermissionDenied"}`)
			})
			state := ""
			if test.filtered {
				state = "Pending"
			}
			result, err := NewSSPAIRService(client).ListJobs(context.Background(), "cn-pj-01", "", state)
			if test.allForbidden {
				if result != nil || err == nil || !strings.Contains(err.Error(), "skip-hostpath-webhook") || !strings.Contains(err.Error(), "readable") || strings.Contains(err.Error(), "https://") {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				return
			}
			if err != nil || result == nil || len(result.Warnings) != 1 || !result.QueryIncomplete {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if (len(result.Items) == 0) != (test.empty || test.filtered) {
				t.Fatalf("items=%+v", result.Items)
			}
			warning := result.Warnings[0]
			if !strings.Contains(warning, "skip-webhook-test") || !strings.Contains(warning, "skip-hostpath-webhook") || strings.Contains(warning, "http://") || strings.Contains(warning, "PermissionDenied") || strings.Contains(warning, "\n") {
				t.Fatal(warning)
			}
		})
	}
}

func TestAIRListCancellationIsStillAnError(t *testing.T) {
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request after cancellation") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := NewSSPAIRService(client).ListJobs(ctx, "cn-pj-01", "", ""); result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
