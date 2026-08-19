package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListVirtualClustersForEnvironmentSearchesAllProfilesByDefault(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		uid := "d-uid"
		if req.URL.Host == "management.pt.test" {
			uid = "pt-uid"
		}
		body := fmt.Sprintf(`{"virtual_clusters":[{"uid":%q,"name":"vc-shared"}]}`, uid)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	client := &VirtualClusterClient{
		currentProfile: "tenant-d",
		profiles: map[string]clientProfile{
			"tenant-d": {
				Name:      "tenant-d",
				AccessKey: "ak",
				SecretKey: "sk",
				BaseURL:   "https://management.d.test",
				Region:    "cn-pj-01",
			},
			"tenant-pt": {
				Name:      "tenant-pt",
				AccessKey: "ak",
				SecretKey: "sk",
				BaseURL:   "https://management.pt.test",
				Region:    "cn-pj-03",
			},
		},
		httpClient: httpClient,
	}

	all, err := client.ListVirtualClustersForEnvironment(context.Background(), "")
	if err != nil {
		t.Fatalf("list all environments: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("list all environments returned %d VCs, want 2", len(all))
	}

	ptOnly, err := client.ListVirtualClustersForEnvironment(context.Background(), "pt")
	if err != nil {
		t.Fatalf("list pt environment: %v", err)
	}
	if len(ptOnly) != 1 || ptOnly[0].UID != "pt-uid" || ptOnly[0].ProfileName != "tenant-pt" {
		t.Fatalf("list pt environment = %#v, want pt VC", ptOnly)
	}
}
