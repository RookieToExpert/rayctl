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

func TestPTATEListUnavailableRequiresAll404(t *testing.T) {
	for _, tt := range []struct {
		name, region string
		status       int
		inventoryErr bool
		cancelled    bool
		want         bool
	}{
		{name: "all404", region: "cn-pj-03", status: 404, want: true},
		{name: "D404", region: "cn-pj-01", status: 404},
		{name: "empty success", region: "cn-pj-03", status: 200},
		{name: "forbidden", region: "cn-pj-03", status: 403},
		{name: "server error", region: "cn-pj-03", status: 500},
		{name: "inventory error", region: "cn-pj-03", status: 404, inventoryErr: true},
		{name: "cancelled", region: "cn-pj-03", status: 404, cancelled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasPrefix(r.URL.Path, "/compute/") {
					if tt.inventoryErr {
						w.WriteHeader(403)
						return
					}
					fmt.Fprint(w, `{"virtual_clusters":[{"uid":"one"},{"uid":"two"}],"total_size":2}`)
					return
				}
				status := 404
				if strings.Contains(r.URL.Path, "vc-two") {
					status = tt.status
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"items":[]}`)
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "platform.json")
			data, _ := json.Marshal(map[string]any{"access_key": "test", "secret_key": "test", "region": tt.region, "base_url": server.URL, "kubernetes_base_url": server.URL})
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
			t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
			client, err := platform.NewVirtualClusterClientFromEnvWithError()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			_, err = NewJobService(nil, nil, client).ListCurrentTenantVolcanoJobs(ctx, false, "", 50)
			if errors.Is(err, ErrPTATEUnavailable) != tt.want {
				t.Fatalf("unexpected classification: %v", err)
			}
			if tt.cancelled && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if tt.want && err.Error() != ErrPTATEUnavailable.Error() {
				t.Fatalf("noisy error: %v", err)
			}
		})
	}
}
