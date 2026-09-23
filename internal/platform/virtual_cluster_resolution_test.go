package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFindExactVirtualClusterUsesDetailEndpoint(t *testing.T) {
	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		wantPath := "/compute/ecp/v1/subscriptions/sub-id/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-test"
		if request.URL.Path != wantPath {
			return nil, fmt.Errorf("path = %q, want %q", request.URL.Path, wantPath)
		}
		return jsonHTTPResponse(request, `{"uid":"019d28e0-9610-74ef-a722-9242dede9e37","name":"vc-test"}`), nil
	})}
	client := &VirtualClusterClient{
		accessKey:      "ak",
		secretKey:      "sk",
		baseURL:        "https://example.test",
		subscription:   "sub-id",
		resourceGroup:  "default",
		region:         "cn-pj-01",
		currentProfile: "test",
		httpClient:     httpClient,
	}

	cluster, err := client.FindExactVirtualCluster(context.Background(), "vc-test")
	if err != nil {
		t.Fatalf("FindExactVirtualCluster() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if cluster.Name != "vc-test" || cluster.UID != "019d28e0-9610-74ef-a722-9242dede9e37" {
		t.Fatalf("cluster = %#v", cluster)
	}
	if cluster.TenantID != "sub-id" || cluster.Region != "cn-pj-01" {
		t.Fatalf("profile metadata was not applied: %#v", cluster)
	}
}

func TestFindExactVirtualClusterUIDDoesNotCallPlatform(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("unexpected request to %s", request.URL)
	})}
	client := &VirtualClusterClient{httpClient: httpClient}

	cluster, err := client.FindExactVirtualCluster(context.Background(), "vc-019d28e0-9610-74ef-a722-9242dede9e37")
	if err != nil {
		t.Fatalf("FindExactVirtualCluster() error = %v", err)
	}
	if cluster.UID != "019d28e0-9610-74ef-a722-9242dede9e37" {
		t.Fatalf("uid = %q", cluster.UID)
	}
}

func TestFindExactVirtualClusterForProfileUsesRequestedProfile(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		wantPath := "/compute/ecp/v1/subscriptions/pt-sub/resourceGroups/default/regions/cn-pj-03/virtualClusters/vc-cpu"
		if request.URL.Path != wantPath {
			return nil, fmt.Errorf("path = %q, want %q", request.URL.Path, wantPath)
		}
		return jsonHTTPResponse(request, `{"uid":"vc-uid","name":"vc-cpu"}`), nil
	})}
	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d":  {Name: "d", Subscription: "d-sub", ResourceGroup: "default", Region: "cn-pj-01"},
			"pt": {Name: "pt", ResourceGroup: "default"},
		},
		httpClient: httpClient,
	}

	cluster, err := client.FindExactVirtualClusterForProfile(context.Background(), "pt", "pt-sub", "cn-pj-03", "vc-cpu")
	if err != nil {
		t.Fatalf("FindExactVirtualClusterForProfile() error = %v", err)
	}
	if cluster.ProfileName != "pt" || cluster.TenantID != "pt-sub" || cluster.Region != "cn-pj-03" {
		t.Fatalf("cluster = %#v", cluster)
	}
}

func TestFindVirtualClusterResourceUsesDetailEndpointAndBuildsScope(t *testing.T) {
	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Path == "/rmh/v1/resources:page" {
			t.Fatal("VC scope lookup must not use RMH")
		}
		wantPath := "/compute/ecp/v1/subscriptions/sub-id/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-a3-deeplink"
		if request.URL.Path != wantPath {
			return nil, fmt.Errorf("path = %q, want %q", request.URL.Path, wantPath)
		}
		return jsonHTTPResponse(request, `{"uid":"vc-uid","name":"vc-a3-deeplink","display_name":"Deeplink"}`), nil
	})}
	client := &VirtualClusterClient{
		accessKey:      "ak",
		secretKey:      "sk",
		baseURL:        "https://example.test",
		subscription:   "sub-id",
		resourceGroup:  "default",
		region:         "cn-pj-01",
		currentProfile: "test",
		httpClient:     httpClient,
	}

	resource, err := client.FindVirtualClusterResource(context.Background(), "vc-a3-deeplink")
	if err != nil {
		t.Fatalf("FindVirtualClusterResource() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	wantRID := "/subscriptions/sub-id/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-a3-deeplink"
	if resource.RID != wantRID {
		t.Fatalf("RID = %q, want %q", resource.RID, wantRID)
	}
	if resource.ProfileName != "test" || resource.ID != "vc-uid" {
		t.Fatalf("resource = %#v", resource)
	}
}

func TestFindVirtualClusterResourceBuildsScopeWhenDetailIsForbidden(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"message":"PermissionDenied"}`)),
			Request:    request,
		}, nil
	})}
	client := &VirtualClusterClient{
		accessKey:      "ak",
		secretKey:      "sk",
		baseURL:        "https://example.test",
		subscription:   "sub-id",
		resourceGroup:  "default",
		region:         "cn-pj-01",
		currentProfile: "test",
		httpClient:     httpClient,
	}

	resource, err := client.FindVirtualClusterResource(context.Background(), "vc-a3-deeplink")
	if err != nil {
		t.Fatalf("FindVirtualClusterResource() error = %v", err)
	}
	wantRID := "/subscriptions/sub-id/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-a3-deeplink"
	if resource.RID != wantRID || resource.ProfileName != "test" {
		t.Fatalf("resource = %#v, want RID %q in profile test", resource, wantRID)
	}
}

func TestFindVirtualClusterResourceContinuesAfterCurrentProfileForbidden(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "/subscriptions/current-sub/") {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"message":"PermissionDenied"}`)),
				Request:    request,
			}, nil
		}
		return jsonHTTPResponse(request, `{"uid":"vc-uid","name":"vc-cross-tenant"}`), nil
	})}
	client := &VirtualClusterClient{
		currentProfile: "current",
		profiles: map[string]clientProfile{
			"current": {Name: "current", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://example.test", Subscription: "current-sub", ResourceGroup: "default", Region: "cn-pj-01"},
			"target":  {Name: "target", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://example.test", Subscription: "target-sub", ResourceGroup: "default", Region: "cn-pj-01"},
		},
		httpClient: httpClient,
	}

	resource, err := client.FindVirtualClusterResource(context.Background(), "vc-cross-tenant")
	if err != nil {
		t.Fatalf("FindVirtualClusterResource() error = %v", err)
	}
	if resource.ProfileName != "target" || !strings.Contains(resource.RID, "/subscriptions/target-sub/") {
		t.Fatalf("resource = %#v, want verified target profile", resource)
	}
}
