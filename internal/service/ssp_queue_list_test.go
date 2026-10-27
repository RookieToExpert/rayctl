package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"rayctl/internal/platform"
)

func TestQueueListResolvesMissingWorkspaceInRunningQueues(t *testing.T) {
	var mu sync.Mutex
	reads := map[string]int{}
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rmh/v1/resources:page" {
			resources := []platform.StorageVolumeResource{}
			for index, properties := range []string{
				`{}`,                                  // Name and UID both missing, despite RUNNING.
				`{"workspace":{"name":"stale-name"}}`, // UID missing.
				`{"workspace":{"uid":"uid-real"}}`,    // Name missing.
				`{"workspace":{"name":"ws-complete","uid":"uid-complete"}}`,
				`{"workspace":{"name":"ws-stale","uid":"uid-stale"}}`,
			} {
				state := "RUNNING"
				if index == 4 {
					state = "CREATING"
				}
				resources = append(resources, platform.StorageVolumeResource{ID: fmt.Sprintf("uid-%d", index), Name: fmt.Sprintf("queue-%d", index), Type: "compute.ssp.v1.queue", State: state, Region: "cn-pj-01", RID: fmt.Sprintf("/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/cluster/queues/queue-%d", index), Properties: properties})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"resources": resources, "total_size": len(resources)})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/clusters/cluster/queues") {
			mu.Lock()
			reads["batch"]++
			mu.Unlock()
			queues := []map[string]any{}
			for index := 0; index < 5; index++ {
				queues = append(queues, map[string]any{"name": fmt.Sprintf("queue-%d", index), "uid": fmt.Sprintf("uid-%d", index), "state": "RUNNING", "properties": map[string]any{"workspace": map[string]string{"name": "ws-real-unrelated-to-queue-name", "uid": "uid-real"}}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"queues": queues, "total_size": len(queues)})
			return
		}
		name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if !strings.Contains(r.URL.Path, "/clusters/cluster/queues/") {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		reads[name]++
		mu.Unlock()
		fmt.Fprintf(w, `{"name":%q,"state":"RUNNING","properties":{"workspace":{"name":"ws-real-unrelated-to-queue-name","uid":"uid-real"},"advanced_settings":{"provide_spot_resource_enabled":false}}}`, name)
	})
	s := NewSSPResourceService(nil, client)
	list, err := s.ListQueues(t.Context(), "cn-pj-01")
	if err != nil || len(list.Items) != 5 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	for _, item := range list.Items {
		if item.State != "RUNNING" {
			t.Fatal(item)
		}
		if item.Name == "queue-3" {
			if item.Workspace != "ws-complete" || item.WorkspaceUID != "uid-complete" {
				t.Fatal(item)
			}
			continue
		}
		if item.Workspace != "ws-real-unrelated-to-queue-name" || item.WorkspaceUID != "uid-real" {
			t.Fatalf("unresolved or inferred workspace: %+v", item)
		}
	}
	mu.Lock()
	if len(reads) != 1 || reads["batch"] != 1 {
		t.Fatalf("unnecessary detail queries: %v", reads)
	}
	for _, count := range reads {
		if count != 1 {
			t.Fatalf("detail fetched repeatedly: %v", reads)
		}
	}
	mu.Unlock()
	get, err := s.GetQueue(t.Context(), "queue-1", "cn-pj-01", false)
	if err != nil || get.Workspace != "ws-real-unrelated-to-queue-name" || get.WorkspaceUID != "uid-real" {
		t.Fatalf("get=%+v err=%v", get, err)
	}
}

func TestQueueListWorkspaceRemainsUnknownWhenPlatformHasNoAssociation(t *testing.T) {
	name := "queue-d-reserved-assumed-workspace"
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rmh/v1/resources:page" {
			fmt.Fprintf(w, `{"resources":[{"id":"queue-uid","name":%q,"type":"compute.ssp.v1.queue","state":"RUNNING","region":"cn-pj-01","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/cluster/queues/%s","properties":"{\"advanced_settings\":{\"provide_spot_resource_enabled\":false}}"}],"total_size":1}`, name, name)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/queues/"+name) {
			fmt.Fprintf(w, `{"name":%q,"state":"RUNNING","properties":{"workspace":{},"advanced_settings":{"provide_spot_resource_enabled":false}}}`, name)
			return
		}
		t.Errorf("unexpected endpoint %s", r.URL.Path)
		http.NotFound(w, r)
	})
	s := NewSSPResourceService(nil, client)
	list, err := s.ListQueues(t.Context(), "cn-pj-01")
	if err != nil || len(list.Items) != 1 || list.Items[0].Workspace != "" || list.Items[0].WorkspaceUID != "" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	get, err := s.GetQueue(t.Context(), name, "cn-pj-01", false)
	if err != nil || get.Workspace != "" || get.WorkspaceUID != "" {
		t.Fatalf("get fabricated a workspace: %+v err=%v", get, err)
	}
}

func TestQueueListDetailsStayInTheirRequestScope(t *testing.T) {
	var mu sync.Mutex
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 13 {
			t.Errorf("unexpected scope %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		profile := "a"
		if strings.Contains(r.Header.Get("Authorization"), `accesskey="b-ak"`) {
			profile = "b"
		}
		ws := "ws-" + profile + "-" + parts[4] + "-" + parts[10]
		state := "RUNNING"
		if profile == "b" {
			state = "DELETING"
		}
		fmt.Fprintf(w, `{"name":"same","state":%q,"properties":{"workspace":{"name":%q,"uid":%q}}}`, state, ws, "uid-"+ws)
	})
	// Reuse this test's in-memory HTTP endpoint with two owning profiles.
	path := platform.DefaultConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Profiles map[string]map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	base := config.Profiles["test"]["base_url"]
	profiles := map[string]any{}
	for _, name := range []string{"a", "b"} {
		profiles[name] = map[string]any{"access_key": name + "-ak", "secret_key": "sk", "subscription_id": "default-sub", "region": "cn-pj-01", "base_url": base}
	}
	data, err = json.Marshal(map[string]any{"current_profile": "a", "profiles": profiles})
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "scopes.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
	client, err = platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	items := []SSPQueueItem{}
	details := []platform.SSPQueueResourceDetails{}
	for _, scope := range []struct{ profile, subscription, cluster string }{{"a", "sub-a", "one"}, {"a", "sub-a", "two"}, {"a", "sub-b", "one"}, {"b", "sub-a", "one"}} {
		item := SSPQueueItem{Name: "same", State: "CREATING", Profile: scope.profile, Subscription: scope.subscription, Region: "cn-pj-01", Cluster: scope.cluster}
		items = append(items, item)
		details = append(details, platform.SSPQueueResourceDetails{Name: item.Name, ProfileName: item.Profile, Subscription: item.Subscription, ResourceGroup: "default", Region: item.Region, ClusterName: item.Cluster})
	}
	if err := NewSSPResourceService(nil, client).refreshQueueListDetails(t.Context(), items, details, "cn-pj-01"); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		want := "ws-" + item.Profile + "-" + item.Subscription + "-" + item.Cluster
		state := "RUNNING"
		if item.Profile == "b" {
			state = "DELETING"
		}
		if item.Workspace != want || item.WorkspaceUID != "uid-"+want || item.State != state {
			t.Fatalf("crossed scope: %+v want %s", item, want)
		}
	}
}

func TestQueueListDetailFailureAndCancellationAreNotSuccess(t *testing.T) {
	for _, test := range []string{"forbidden", "wrong name", "wrong uid", "cancelled"} {
		t.Run(test, func(t *testing.T) {
			client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch test {
				case "forbidden":
					w.WriteHeader(403)
				case "wrong name":
					fmt.Fprint(w, `{"name":"another","uid":"expected"}`)
				case "wrong uid":
					fmt.Fprint(w, `{"name":"same","uid":"another"}`)
				default:
					t.Error("cancelled request reached server")
				}
			})
			ctx := t.Context()
			if test == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			items := []SSPQueueItem{{Name: "same", UID: "expected", State: "RUNNING"}}
			details := []platform.SSPQueueResourceDetails{{Name: "same", ProfileName: "test", Subscription: "sub", ClusterName: "cluster", Region: "cn-pj-01"}}
			err := NewSSPResourceService(nil, client).refreshQueueListDetails(ctx, items, details, "cn-pj-01")
			if err == nil || items[0].Workspace != "" {
				t.Fatalf("items=%+v err=%v", items, err)
			}
			if test == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestQueueListBatchUsesIndividualDetailsOnlyForMissingEntries(t *testing.T) {
	for _, forbidden := range []bool{false, true} {
		t.Run(fmt.Sprint(forbidden), func(t *testing.T) {
			var mu sync.Mutex
			reads := map[string]int{}
			client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				mu.Lock()
				reads[name]++
				mu.Unlock()
				if name == "queues" {
					if forbidden {
						w.WriteHeader(403)
						return
					}
					fmt.Fprint(w, `{"queues":[{"name":"queue-1","uid":"uid-1","state":"RUNNING","properties":{"workspace":{"name":"ws-real","uid":"ws-uid"}}},{"name":"queue-2","uid":"uid-2","state":"RUNNING","workspace_name":"incomplete-name"}],"total_size":2}`)
					return
				}
				fmt.Fprintf(w, `{"name":%q,"state":"RUNNING","properties":{"workspace":{"name":"ws-real","uid":"ws-uid"}}}`, name)
			})
			items := []SSPQueueItem{}
			details := []platform.SSPQueueResourceDetails{}
			for i := 1; i <= 3; i++ {
				name, uid := fmt.Sprintf("queue-%d", i), fmt.Sprintf("uid-%d", i)
				items = append(items, SSPQueueItem{Name: name, UID: uid, State: "RUNNING"})
				details = append(details, platform.SSPQueueResourceDetails{Name: name, UID: uid, ProfileName: "test", Subscription: "sub", ResourceGroup: "default", Region: "cn-pj-01", ClusterName: "cluster"})
			}
			err := NewSSPResourceService(nil, client).refreshQueueListDetails(t.Context(), items, details, "cn-pj-01")
			mu.Lock()
			defer mu.Unlock()
			if forbidden {
				if err == nil || len(reads) != 1 || reads["queues"] != 1 {
					t.Fatalf("batch failure amplified: reads=%v err=%v", reads, err)
				}
				return
			}
			if err != nil || reads["queues"] != 1 || reads["queue-1"] != 0 || reads["queue-2"] != 1 || reads["queue-3"] != 1 {
				t.Fatalf("reads=%v err=%v", reads, err)
			}
			for _, item := range items {
				if item.Workspace != "ws-real" || item.WorkspaceUID != "ws-uid" {
					t.Fatal(item)
				}
			}
		})
	}
}
