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
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"rayctl/internal/platform"
)

func TestPTATEUnavailableRequiresCompleteAll404Inventory(t *testing.T) {
	for _, test := range []struct {
		name            string
		region          string
		secondStatus    int
		inventoryFails  bool
		emptyInventory  bool
		cancelled       bool
		wantUnavailable bool
	}{
		{name: "PT all endpoints absent", region: "cn-pj-03", secondStatus: 404, wantUnavailable: true},
		{name: "D 404 remains an error", region: "cn-pj-01", secondStatus: 404},
		{name: "PT some endpoints work", region: "cn-pj-03", secondStatus: 200},
		{name: "PT permission error", region: "cn-pj-03", secondStatus: 403},
		{name: "PT inventory incomplete", region: "cn-pj-03", secondStatus: 404, inventoryFails: true},
		{name: "PT empty inventory is inconclusive", region: "cn-pj-03", emptyInventory: true},
		{name: "PT cancellation is preserved", region: "cn-pj-03", secondStatus: 404, cancelled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var hcRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasPrefix(r.URL.Path, "/compute/") {
					second := strings.Contains(r.Header.Get("Authorization"), `accesskey="two"`)
					if second && test.inventoryFails {
						w.WriteHeader(403)
						fmt.Fprint(w, `{"message":"inventory forbidden"}`)
						return
					}
					if test.emptyInventory {
						fmt.Fprint(w, `{"virtual_clusters":[]}`)
						return
					}
					uid := "one"
					if second {
						uid = "two"
					}
					fmt.Fprintf(w, `{"virtual_clusters":[{"uid":%q}]}`, uid)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/ecp/") {
					status := 404
					if strings.Contains(r.URL.Path, "/vc-two/") {
						status = test.secondStatus
					}
					if status != 200 {
						w.WriteHeader(status)
						fmt.Fprint(w, `{"message":"upstream previously returned 404: unavailable"}`)
						return
					}
					fmt.Fprint(w, `{"kind":"JobList","items":[]}`)
					return
				}
				hcRequests.Add(1)
				if r.URL.Query().Get("fieldSelector") != "" || (r.URL.Query().Get("labelSelector") != "" && r.URL.Query().Get("labelSelector") != atePodLookupSelector) {
					json.NewEncoder(w).Encode(corev1.PodList{Items: []corev1.Pod{}})
				} else {
					writeLookupMetadata(w, nil, "")
				}
			}))
			defer server.Close()
			profiles := map[string]any{}
			for _, name := range []string{"one", "two"} {
				profiles[name] = map[string]any{"access_key": name, "secret_key": "sk", "base_url": server.URL, "kubernetes_base_url": server.URL, "region": test.region}
			}
			path := filepath.Join(t.TempDir(), "platform.json")
			config, _ := json.Marshal(map[string]any{"current_profile": "one", "profiles": profiles})
			if err := os.WriteFile(path, config, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
			t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
			vc, err := platform.NewVirtualClusterClientFromEnvWithError()
			if err != nil {
				t.Fatal(err)
			}
			restConfig := &rest.Config{Host: server.URL, QPS: 100, Burst: 100}
			kube, err := kubernetes.NewForConfig(restConfig)
			if err != nil {
				t.Fatal(err)
			}
			dyn, err := dynamic.NewForConfig(restConfig)
			if err != nil {
				t.Fatal(err)
			}
			s := NewJobService(kube, dyn, vc)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancelled {
				cancel()
			}
			_, err = s.GetJob(ctx, "missing-task")
			if errors.Is(err, ErrPTATEUnavailable) != test.wantUnavailable {
				t.Fatalf("wrong service availability: %v", err)
			}
			if test.wantUnavailable {
				if err.Error() != "PT 环境没有 ATE 服务，请用 ait" || hcRequests.Load() != 0 {
					t.Fatalf("unexpected fallback or noisy error: %v HC=%d", err, hcRequests.Load())
				}
			} else if test.cancelled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
			} else if !test.emptyInventory {
				var incomplete *jobLookupError
				if !errors.As(err, &incomplete) || !strings.Contains(err.Error(), "returned ") {
					t.Fatalf("backend error lost: %v", err)
				}
			}
		})
	}
}
