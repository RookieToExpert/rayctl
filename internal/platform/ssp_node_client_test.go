package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestListSSPClustersForNodeUsageScopesSortsAndDeduplicates(t *testing.T) {
	var requests []string
	client := &VirtualClusterClient{
		currentProfile: "tenant-pt",
		profiles: map[string]clientProfile{
			"tenant-d":     {Name: "tenant-d", BaseURL: "https://tenant-d.example", AccessKey: "d-ak", SecretKey: "sk", Region: "cn-pj-01", Subscription: "d-sub"},
			"tenant-pt":    {Name: "tenant-pt", BaseURL: "https://tenant-d.example", AccessKey: "pt-ak", SecretKey: "sk", Region: "cn-pj-01", Subscription: "pt-sub"},
			"tenant-cloud": {Name: "tenant-cloud", BaseURL: "https://tenant-cloud.example", AccessKey: "cloud-ak", SecretKey: "sk", Region: "cn-pj-03"},
			"other-d":      {Name: "other-d", BaseURL: "https://other-d.example", AccessKey: "other-ak", SecretKey: "sk", Region: "cn-pj-01"},
		},
		httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			requestProfile := "tenant-d.example"
			if strings.Contains(r.Header.Get("Authorization"), `accesskey="pt-ak"`) {
				requestProfile = "tenant-pt.example"
			}
			requests = append(requests, requestProfile)
			if r.URL.Path != "/compute/ssp/v1/subscriptions/-/resourceGroups/-/regions/-/clusters" || r.URL.Query().Get("page_size") != "100" || r.URL.Query().Get("skip") != "0" {
				t.Fatalf("unexpected URL %s", r.URL)
			}
			var body string
			switch requestProfile {
			case "tenant-pt.example":
				body = `{"clusters":[{"name":"z-cluster","uid":"z-uid"},{"name":"shared","uid":"shared-uid"},{"name":"duplicate-uid","uid":"SHARED-UID"},{"name":"name-only"},{"name":"NAME-ONLY"},{"name":"other-region","uid":"other-uid","region":"cn-pj-03"}],"total_size":6}`
			case "tenant-d.example":
				body = `{"clusters":[{"name":"shared","uid":"shared-uid"},{"name":"a-cluster","uid":"a-uid"}],"total_size":2}`
			default:
				t.Fatalf("queried an unrelated profile: %s", r.URL.Host)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})},
	}
	clusters, err := client.ListSSPClustersForNodeUsage(context.Background(), "cn-pj-01")
	if err != nil {
		t.Fatal(err)
	}
	wantRequests := []string{"tenant-pt.example", "tenant-d.example"}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("requests = %v, want %v", requests, wantRequests)
	}
	var identities []string
	for _, cluster := range clusters {
		identities = append(identities, cluster.Name+"|"+cluster.ProfileName)
		if cluster.Region != "cn-pj-01" || cluster.Subscription == "" || cluster.ResourceGroup != "default" {
			t.Fatalf("cluster location was not normalized: %#v", cluster)
		}
	}
	want := []string{"a-cluster|tenant-d", "name-only|tenant-pt", "shared|tenant-d", "shared|tenant-pt", "z-cluster|tenant-pt"}
	if !reflect.DeepEqual(identities, want) {
		t.Fatalf("cluster identities = %v, want %v", identities, want)
	}
}

func TestListSSPClustersForNodeUsageDiscardsPartialInventory(t *testing.T) {
	for _, failure := range []string{"later-profile", "later-page"} {
		t.Run(failure, func(t *testing.T) {
			var requests []string
			client := &VirtualClusterClient{
				currentProfile: "tenant-d",
				profiles: map[string]clientProfile{
					"tenant-d":  {Name: "tenant-d", BaseURL: "https://tenant-d.example", AccessKey: "d-ak", SecretKey: "sk", Region: "cn-pj-01"},
					"tenant-pt": {Name: "tenant-pt", BaseURL: "https://tenant-d.example", AccessKey: "pt-ak", SecretKey: "sk", Region: "cn-pj-01"},
				},
				httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					requestProfile := "tenant-d.example"
					if strings.Contains(r.Header.Get("Authorization"), `accesskey="pt-ak"`) {
						requestProfile = "tenant-pt.example"
					}
					requests = append(requests, requestProfile+"|"+r.URL.Query().Get("skip"))
					fail := failure == "later-profile" && requestProfile == "tenant-pt.example" || failure == "later-page" && r.URL.Query().Get("skip") == "100"
					if fail {
						return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(`{"message":"inventory unavailable"}`)), Request: r}, nil
					}
					clusters := []map[string]string{{"name": "complete-first-profile", "uid": "first-uid"}}
					total := 1
					if failure == "later-page" {
						total = 101
						clusters = make([]map[string]string, 100)
						for i := range clusters {
							clusters[i] = map[string]string{"name": fmt.Sprintf("cluster-%d", i), "uid": fmt.Sprintf("uid-%d", i)}
						}
					}
					body, err := json.Marshal(map[string]any{"clusters": clusters, "total_size": total})
					if err != nil {
						t.Fatal(err)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
				})},
			}
			clusters, err := client.ListSSPClustersForNodeUsage(context.Background(), "cn-pj-01")
			if err == nil || clusters != nil {
				t.Fatalf("clusters = %#v, error = %v; want no partial inventory and an error", clusters, err)
			}
			failedProfile := "tenant-pt"
			wantRequests := []string{"tenant-d.example|0", "tenant-pt.example|0"}
			if failure == "later-page" {
				failedProfile = "tenant-d"
				wantRequests = []string{"tenant-d.example|0", "tenant-d.example|100"}
			}
			if !strings.Contains(err.Error(), failedProfile) || !reflect.DeepEqual(requests, wantRequests) {
				t.Fatalf("requests = %v, error = %v; want %v and profile %q", requests, err, wantRequests, failedProfile)
			}
		})
	}
}

func TestListSSPClustersForNodeUsageRejectsUnconfiguredRegion(t *testing.T) {
	client := &VirtualClusterClient{profiles: map[string]clientProfile{"d": {Name: "d", Region: "cn-pj-01"}}}
	clusters, err := client.ListSSPClustersForNodeUsage(context.Background(), "cn-pj-03")
	if err == nil || clusters != nil {
		t.Fatalf("clusters = %#v, error = %v", clusters, err)
	}
}

func TestListSSPClustersForNodeUsageValidatesPagination(t *testing.T) {
	tests := []struct {
		name      string
		pages     []string
		wantError string
		wantNames []string
	}{
		{name: "missing-array", pages: []string{`{}`}, wantError: "clusters array"},
		{name: "null-array", pages: []string{`{"clusters":null,"total_size":0}`}, wantError: "clusters array"},
		{name: "missing-cluster-identity", pages: []string{`{"clusters":[{"name":"first","uid":"first"},{}],"total_size":2}`}, wantError: "no UID or name"},
		{name: "null-cluster", pages: []string{`{"clusters":[null],"total_size":1}`}, wantError: "no UID or name"},
		{name: "name-from-resource-id", pages: []string{`{"clusters":[{"id":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/id-name"}],"total_size":1}`}, wantNames: []string{"id-name"}},
		{name: "empty", pages: []string{`{"clusters":[],"total_size":0}`}, wantNames: []string{}},
		{name: "empty-without-total", pages: []string{`{"clusters":[]}`}, wantNames: []string{}},
		{name: "negative-total", pages: []string{`{"clusters":[],"total_size":-1}`}, wantError: "total_size is inconsistent"},
		{name: "exceeds-total", pages: []string{`{"clusters":[{"name":"first","uid":"first"}],"total_size":0}`}, wantError: "exceeds total_size"},
		{
			name: "short-page-total-continues",
			pages: []string{
				`{"clusters":[{"name":"first","uid":"first"}],"total_size":2}`,
				`{"clusters":[{"name":"second","uid":"second"}],"total_size":2}`,
			},
			wantNames: []string{"first", "second"},
		},
		{
			name: "empty-before-total",
			pages: []string{
				`{"clusters":[{"name":"first","uid":"first"}],"total_size":2}`,
				`{"clusters":[],"total_size":2}`,
			},
			wantError: "empty page before pagination finished",
		},
		{
			name: "changed-total",
			pages: []string{
				`{"clusters":[{"name":"first","uid":"first"}],"total_size":2}`,
				`{"clusters":[{"name":"second","uid":"second"}],"total_size":3}`,
			},
			wantError: "total_size is inconsistent",
		},
		{
			name: "token-continues",
			pages: []string{
				`{"clusters":[{"name":"first","uid":"first"}],"next_page_token":"next"}`,
				`{"clusters":[{"name":"second","uid":"second"}]}`,
			},
			wantNames: []string{"first", "second"},
		},
		{
			name: "repeated-token",
			pages: []string{
				`{"clusters":[{"name":"first","uid":"first"}],"next_page_token":"next"}`,
				`{"clusters":[{"name":"second","uid":"second"}],"next_page_token":"next"}`,
			},
			wantError: "repeated page token",
		},
		{
			name: "repeated-page",
			pages: []string{
				`{"clusters":[{"name":"first","uid":"first"}],"total_size":3}`,
				`{"clusters":[{"name":"first","uid":"first"}],"total_size":3}`,
			},
			wantError: "repeated page",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &VirtualClusterClient{
				profiles: map[string]clientProfile{"d": {Name: "d", BaseURL: "https://d.example", Region: "cn-pj-01"}},
				httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					if requests >= len(test.pages) {
						t.Fatalf("unexpected extra request: %s", r.URL)
					}
					if r.URL.Query().Get("skip") != fmt.Sprint(requests) {
						t.Errorf("skip = %q, want %d", r.URL.Query().Get("skip"), requests)
					}
					if requests > 0 && strings.Contains(test.pages[requests-1], `"next_page_token":"next"`) && r.URL.Query().Get("page_token") != "next" {
						t.Errorf("page token = %q, want next", r.URL.Query().Get("page_token"))
					}
					body := test.pages[requests]
					requests++
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})},
			}
			clusters, err := client.ListSSPClustersForNodeUsage(context.Background(), "cn-pj-01")
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || clusters != nil {
					t.Fatalf("clusters = %#v, error = %v; want nil inventory and %q", clusters, err, test.wantError)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				names := make([]string, 0, len(clusters))
				for _, cluster := range clusters {
					names = append(names, cluster.Name)
				}
				if !reflect.DeepEqual(names, test.wantNames) {
					t.Fatalf("names = %v, want %v", names, test.wantNames)
				}
			}
			if requests != len(test.pages) {
				t.Fatalf("requests = %d, want %d", requests, len(test.pages))
			}
		})
	}
}

func TestListSSPClustersForNodeUsageRejectsCanceledSuccessfulResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &VirtualClusterClient{
		profiles: map[string]clientProfile{"d": {Name: "d", BaseURL: "https://d.example", Region: "cn-pj-01"}},
		httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			cancel()
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"clusters":[{"name":"first","uid":"first"}],"total_size":1}`)), Request: r}, nil
		})},
	}
	clusters, err := client.ListSSPClustersForNodeUsage(ctx, "cn-pj-01")
	if !errors.Is(err, context.Canceled) || clusters != nil {
		t.Fatalf("clusters = %#v, error = %v; want nil inventory and context.Canceled", clusters, err)
	}
}
