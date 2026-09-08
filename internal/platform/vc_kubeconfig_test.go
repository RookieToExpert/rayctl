package platform

import (
	"context"
	"io"
	"net/http"
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
