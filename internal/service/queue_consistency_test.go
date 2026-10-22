package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rayctl/internal/platform"
)

func TestQueueListMatchesGetAndKeepsFiveQueuesWithoutWorkspaceMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rmh/v1/resources:page":
			resources := []platform.StorageVolumeResource{}
			for i := 0; i < 6; i++ {
				properties := `{"advanced_settings":{"provide_spot_resource_enabled":false}}`
				if i == 0 {
					properties = `{"workspace":{"name":"ws-demo"},"advanced_settings":{"provide_spot_resource_enabled":false}}`
				}
				resources = append(resources, platform.StorageVolumeResource{ID: fmt.Sprintf("uid-%d", i), Name: fmt.Sprintf("queue-%d", i), Type: "compute.ssp.v1.queue", State: "CREATING", Region: "cn-pj-01", RID: fmt.Sprintf("/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/cluster/queues/queue-%d", i), Properties: properties})
			}
			json.NewEncoder(w).Encode(map[string]any{"resources": resources, "total_size": 6})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/clusters/cluster/queues/queue-"):
			name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			fmt.Fprintf(w, `{"name":%q,"state":"RUNNING","workspace_name":"ws-demo","properties":{"advanced_settings":{"provide_spot_resource_enabled":false}}}`, name)
		default:
			t.Error("unexpected endpoint", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	data, _ := json.Marshal(map[string]any{"current_profile": "test", "profiles": map[string]any{"test": map[string]any{"access_key": "ak", "secret_key": "sk", "base_url": server.URL, "region": "cn-pj-01", "subscription_id": "sub"}}})
	path := filepath.Join(t.TempDir(), "platform.json")
	os.WriteFile(path, data, 0600)
	t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	s := NewSSPResourceService(nil, client)
	list, err := s.ListQueues(context.Background(), "cn-pj-01")
	if err != nil || len(list.Items) != 6 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	get, err := s.GetQueue(context.Background(), "queue-0", "cn-pj-01", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if item.State != "RUNNING" || item.State != get.State {
			t.Fatalf("list/get mismatch: %+v / %+v", item, get)
		}
	}
}

func TestQueueAuthoritativeStateDoesNotCrossTenantForSameName(t *testing.T) {
	items := []SSPQueueItem{{Profile: "a", Name: "same", State: "CREATING"}, {Profile: "b", Name: "same", State: "CREATING"}}
	queues := []platform.SSPQueue{{ProfileName: "a", Name: "same", State: "RUNNING"}, {ProfileName: "b", Name: "same", State: "DELETING"}}
	if err := mergeAuthoritativeQueueStates(items, []string{"a/same", "b/same"}, queues); err != nil || items[0].State != "RUNNING" || items[1].State != "DELETING" {
		t.Fatal(items, err)
	}
}
