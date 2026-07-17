package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestFindIAMResourceScope(t *testing.T) {
	clientHTTP := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/iam/authz/v1/services/rm/levels/resources/scopes" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-test" {
			t.Fatalf("Authorization = %q", got)
		}
		if filter := r.URL.Query().Get("filter"); !strings.Contains(filter, `name="*vpc*"`) {
			t.Fatalf("filter = %q", filter)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"scopes": [{
					"name": "vpc-muxi-ailab",
					"display_name": "Muxi VPC",
					"scope": "/rm/subscriptions/sub/resourceGroups/default/zones/cn-pj-01a/vpcs/vpc-muxi-ailab"
				}]
			}`)),
			Request: r,
		}, nil
	})}

	client := &VirtualClusterClient{
		httpClient:     clientHTTP,
		currentProfile: "test",
		profiles: map[string]clientProfile{
			"test": {Name: "test", IAMBaseURL: "https://iam.example.test"},
		},
	}
	resource, err := client.FindIAMResourceScope(context.Background(), "vpc-muxi-ailab", "vpc", "token-test")
	if err != nil {
		t.Fatalf("FindIAMResourceScope returned error: %v", err)
	}
	if resource.Name != "vpc-muxi-ailab" {
		t.Fatalf("resource name = %q", resource.Name)
	}
	if resource.RID != "/rm/subscriptions/sub/resourceGroups/default/zones/cn-pj-01a/vpcs/vpc-muxi-ailab" {
		t.Fatalf("resource scope = %q", resource.RID)
	}
}
