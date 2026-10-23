package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListSSPClustersForProfileScopesAndPaginates(t *testing.T) {
	requests := 0
	client := &VirtualClusterClient{
		currentProfile: "other",
		profiles: map[string]clientProfile{
			"target": {Name: "target", BaseURL: "https://target.example", AccessKey: "ak", SecretKey: "sk", Subscription: "sub", Region: "cn-pj-01"},
			"other":  {Name: "other", BaseURL: "https://other.example", AccessKey: "other-ak", SecretKey: "other-sk"},
		},
		httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			if r.URL.Host != "target.example" || r.URL.Path != "/compute/ssp/v1/subscriptions/-/resourceGroups/-/regions/-/clusters" {
				t.Errorf("unexpected URL %s", r.URL)
			}
			var body strings.Builder
			body.WriteString(`{"clusters":[`)
			if r.URL.Query().Get("skip") == "0" {
				for index := range 100 {
					if index > 0 {
						body.WriteString(",")
					}
					fmt.Fprintf(&body, `{"name":"ssp-%d","uid":"uid-%d"}`, index, index)
				}
			} else {
				body.WriteString(`{"name":"ssp-last","uid":"last","region":"cn-pj-01"},{"name":"other-region","uid":"other","region":"cn-pj-03"}`)
			}
			body.WriteString(`],"total_size":102}`)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body.String())), Request: r}, nil
		})},
	}
	items, err := client.ListSSPClustersForProfile(context.Background(), "target", "cn-pj-01")
	if err != nil || len(items) != 101 || requests != 2 {
		t.Fatalf("items=%d requests=%d error=%v", len(items), requests, err)
	}
	for _, item := range items {
		if item.ProfileName != "target" || item.Subscription != "sub" || item.ResourceGroup != "default" || item.Region != "cn-pj-01" {
			t.Fatalf("cluster location = %#v", item)
		}
	}
}
