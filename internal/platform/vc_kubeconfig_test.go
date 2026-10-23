package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestVCDeletionNeverUsesPermissionErrorsOrDifferentUID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"forbidden", 403, `{}`, false}, {"missing", 404, `{}`, false}, {"gone", 410, `{}`, false},
		{"different_uid", 200, `{"uid":"other","state":"DELETED"}`, false},
		{"running", 200, `{"uid":"original","state":"RUNNING"}`, false},
		{"deleted", 200, `{"uid":"original","state":"DELETED"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &VirtualClusterClient{profiles: map[string]clientProfile{"owner": {Name: "owner", BaseURL: "https://management.example.test", AccessKey: "ak", SecretKey: "sk"}}, httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if !strings.Contains(r.URL.Path, "/subscriptions/sub/resourceGroups/rg/regions/region/virtualClusters/vc-test") {
					t.Fatal(r.URL.Path)
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})}}
			deleted, _ := c.VCKubeconfigDeleted(context.Background(), VCKubeconfigRef{Name: "vc-test", UID: "original", Profile: "owner", Subscription: "sub", ResourceGroup: "rg", Region: "region"})
			if deleted != tc.want {
				t.Fatalf("deleted=%v", deleted)
			}
		})
	}
}

func vcInventoryTestClient(respond func(*http.Request) (*http.Response, error)) *VirtualClusterClient {
	return &VirtualClusterClient{
		currentProfile: "owner",
		profiles: map[string]clientProfile{
			"owner": {Name: "owner", BaseURL: "https://management.example.test", AccessKey: "ak", SecretKey: "sk", Region: "cn-pj-01"},
		},
		httpClient: &http.Client{Transport: roundTripFunc(respond)},
	}
}

func vcInventoryTestPage(start, count int, metadata map[string]any) string {
	items := make([]VirtualCluster, 0, count)
	for i := start; i < start+count; i++ {
		items = append(items, VirtualCluster{UID: fmt.Sprintf("uid-%d", i), Name: fmt.Sprintf("vc-%d", i), TenantID: "sub", Region: "cn-pj-01"})
	}
	payload := map[string]any{"virtual_clusters": items}
	for key, value := range metadata {
		payload[key] = value
	}
	body, _ := json.Marshal(payload)
	return string(body)
}

func TestVCKubeconfigInventoryRequiresValidResourceArray(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"empty virtual_clusters", `{"virtual_clusters":[]}`, true},
		{"empty items", `{"items":[],"total_size":0}`, true},
		{"missing array", `{}`, false},
		{"null virtual_clusters", `{"virtual_clusters":null}`, false},
		{"null items", `{"items":null}`, false},
		{"wrong array type", `{"virtual_clusters":{}}`, false},
		{"null response", `null`, false},
		{"negative total", `{"virtual_clusters":[],"total_size":-1}`, false},
		{"empty but unfinished", `{"virtual_clusters":[],"total_size":1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := vcInventoryTestClient(func(r *http.Request) (*http.Response, error) {
				return jsonHTTPResponse(r, tc.body), nil
			})
			refs, failures := c.VCKubeconfigInventory(context.Background(), "d")
			if (len(failures) == 0) != tc.valid || len(refs) != 0 {
				t.Fatalf("refs=%v failures=%v valid=%v", refs, failures, tc.valid)
			}
		})
	}
}

func TestVCKubeconfigInventoryRequiresRelevantProfiles(t *testing.T) {
	for _, tc := range []struct {
		name, environment string
		profiles          map[string]clientProfile
	}{
		{name: "no configured profiles", environment: "all"},
		{name: "no D/PT profile", environment: "d", profiles: map[string]clientProfile{"cloud": {Name: "cloud", BaseURL: "https://management-cloud.d.pjlab.org.cn"}}},
		{name: "no cloud profile", environment: "dcloud", profiles: map[string]clientProfile{"d": {Name: "d", BaseURL: "https://management.d.pjlab.org.cn"}}},
		{name: "unknown environment", environment: "other", profiles: map[string]clientProfile{"d": {Name: "d", BaseURL: "https://management.d.pjlab.org.cn"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := vcInventoryTestClient(func(r *http.Request) (*http.Response, error) {
				t.Fatal("queried a profile outside the selected environment")
				return nil, nil
			})
			c.profiles = tc.profiles
			refs, failures := c.VCKubeconfigInventory(context.Background(), tc.environment)
			if len(refs) != 0 || len(failures) == 0 {
				t.Fatalf("refs=%v failures=%v", refs, failures)
			}
		})
	}
}

func TestVCKubeconfigInventoryPagination(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pages      []string
		wantSkips  []int
		wantTokens []string
		wantCount  int
	}{
		{
			name:      "short pages with total_size",
			pages:     []string{vcInventoryTestPage(0, 1, map[string]any{"total_size": 2}), vcInventoryTestPage(1, 1, map[string]any{"total_size": 2})},
			wantSkips: []int{0, 1}, wantTokens: []string{"", ""}, wantCount: 2,
		},
		{
			name:      "short pages with cursor",
			pages:     []string{vcInventoryTestPage(0, 1, map[string]any{"next_page_token": "cursor-2"}), vcInventoryTestPage(1, 1, nil)},
			wantSkips: []int{0, 1}, wantTokens: []string{"", "cursor-2"}, wantCount: 2,
		},
		{
			name:      "full page without metadata",
			pages:     []string{vcInventoryTestPage(0, 100, nil), vcInventoryTestPage(100, 1, nil)},
			wantSkips: []int{0, 100}, wantTokens: []string{"", ""}, wantCount: 101,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := vcInventoryTestClient(func(r *http.Request) (*http.Response, error) {
				if calls >= len(tc.pages) {
					t.Fatal("requested an unexpected trailing page")
				}
				q := r.URL.Query()
				if q.Get("page_size") != "100" || q.Get("skip") != strconv.Itoa(tc.wantSkips[calls]) || q.Get("page_token") != tc.wantTokens[calls] {
					t.Fatalf("page %d query=%v", calls, q)
				}
				body := tc.pages[calls]
				calls++
				return jsonHTTPResponse(r, body), nil
			})
			refs, failures := c.VCKubeconfigInventory(context.Background(), "d")
			if len(failures) != 0 || len(refs) != tc.wantCount || calls != len(tc.pages) {
				t.Fatalf("count=%d failures=%v calls=%d", len(refs), failures, calls)
			}
		})
	}
}

func TestVCKubeconfigInventoryRejectsIncompletePagination(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second string
		secondStatus  int
	}{
		{"later request failure", vcInventoryTestPage(0, 1, map[string]any{"total_size": 2}), `{}`, http.StatusForbidden},
		{"later schema failure", vcInventoryTestPage(0, 1, map[string]any{"total_size": 2}), `{}`, http.StatusOK},
		{"later empty page", vcInventoryTestPage(0, 1, map[string]any{"total_size": 2}), `{"virtual_clusters":[],"total_size":2}`, http.StatusOK},
		{"changed total", vcInventoryTestPage(0, 1, map[string]any{"total_size": 2}), vcInventoryTestPage(1, 1, map[string]any{"total_size": 3}), http.StatusOK},
		{"repeated token", vcInventoryTestPage(0, 1, map[string]any{"next_page_token": "next"}), vcInventoryTestPage(1, 1, map[string]any{"next_page_token": "next"}), http.StatusOK},
		{"repeated page", vcInventoryTestPage(0, 100, nil), vcInventoryTestPage(0, 100, nil), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := vcInventoryTestClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return jsonHTTPResponse(r, tc.first), nil
				}
				if calls > 2 {
					t.Fatal("did not stop after incomplete pagination")
				}
				response := jsonHTTPResponse(r, tc.second)
				response.StatusCode = tc.secondStatus
				return response, nil
			})
			refs, failures := c.VCKubeconfigInventory(context.Background(), "d")
			if len(refs) != 0 || len(failures) == 0 || calls != 2 {
				t.Fatalf("refs=%v failures=%v calls=%d", refs, failures, calls)
			}
		})
	}
}

func TestVCKubeconfigInventoryFiltersResourceEnvironment(t *testing.T) {
	for _, environment := range []string{"d", "pt", "dcloud", "all"} {
		t.Run(environment, func(t *testing.T) {
			calls := map[string]int{}
			c := vcInventoryTestClient(func(r *http.Request) (*http.Response, error) {
				calls[r.URL.Host]++
				body := `{"virtual_clusters":[{"uid":"d-uid","name":"vc-d","tenant_id":"sub","region":"cn-pj-01"},{"uid":"pt-uid","name":"vc-pt","tenant_id":"sub","region":"cn-pj-03"}]}`
				if r.URL.Host == "management-cloud.d.pjlab.org.cn" {
					body = `{"virtual_clusters":[{"uid":"cloud-uid","name":"vc-cloud","tenant_id":"sub","region":"cn-pj-01"}]}`
				}
				return jsonHTTPResponse(r, body), nil
			})
			c.profiles["pt"] = clientProfile{Name: "pt", BaseURL: "https://management.pt.test", Region: "cn-pj-03"}
			c.profiles["cloud"] = clientProfile{Name: "cloud", BaseURL: "https://management-cloud.d.pjlab.org.cn"}
			refs, failures := c.VCKubeconfigInventory(context.Background(), environment)
			if len(failures) != 0 {
				t.Fatal(failures)
			}
			wantCount, wantCalls := 1, 2
			if environment == "all" {
				wantCount, wantCalls = 3, 3
			} else if environment == "dcloud" {
				wantCalls = 1
			}
			if len(refs) != wantCount || len(calls) != wantCalls {
				t.Fatalf("refs=%v calls=%v", refs, calls)
			}
			for _, ref := range refs {
				if environment != "all" && ref.Environment != environment {
					t.Fatalf("wrong environment: %#v", ref)
				}
			}
			if environment == "d" || environment == "pt" {
				if calls["management-cloud.d.pjlab.org.cn"] != 0 || calls["management.example.test"] != 1 || calls["management.pt.test"] != 1 {
					t.Fatalf("D/PT must query both tenant endpoints without cloud: %v", calls)
				}
			}
		})
	}
}

func TestVCKubeconfigInventoryRejectsOverlappingPages(t *testing.T) {
	pages := []string{
		vcInventoryTestPage(0, 2, map[string]any{"total_size": 4}),
		vcInventoryTestPage(1, 2, map[string]any{"total_size": 4}),
	}
	calls := 0
	client := vcInventoryTestClient(func(r *http.Request) (*http.Response, error) {
		if calls >= len(pages) {
			t.Fatal("unexpected extra page")
		}
		body := pages[calls]
		calls++
		return jsonHTTPResponse(r, body), nil
	})
	refs, failures := client.VCKubeconfigInventory(context.Background(), "d")
	if len(failures) == 0 || len(refs) != 0 {
		t.Fatalf("overlapping pages accepted as complete: refs=%v failures=%v", refs, failures)
	}
}
