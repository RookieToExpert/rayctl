package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBatchGetVirtualClusterNodeResourceUsages(t *testing.T) {
	clientHTTP := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		wantPath := "/compute/ecp/v1/subscriptions/sub-id/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-test/nodePools/-/nodes/-/resourceUsages:batchGet"
		if request.URL.Path != wantPath {
			t.Fatalf("request path = %q, want %q", request.URL.Path, wantPath)
		}
		if got := request.URL.Query().Get("request_id"); got != "vc-uid" {
			t.Fatalf("request_id = %q, want vc-uid", got)
		}
		if got := request.URL.Query()["node_uids"]; len(got) != 2 || got[0] != "node-1" || got[1] != "node-2" {
			t.Fatalf("node_uids = %#v, want [node-1 node-2]", got)
		}
		body := `{"node_usages":[{"uid":"node-1","machine_name":"host-1","usage":{"total":{"cpu":"16","memory":"64GiB","device":"8"},"allocated":{"cpu":"8","memory":"32GiB","device":"4"},"available":{"cpu":"8","memory":"32GiB","device":"4"}}}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	client := &VirtualClusterClient{
		currentProfile: "test",
		profiles: map[string]clientProfile{
			"test": {
				Name:          "test",
				AccessKey:     "ak",
				SecretKey:     "sk",
				BaseURL:       "https://management.example.test",
				ResourceGroup: "default",
			},
		},
		httpClient: clientHTTP,
	}

	result, err := client.BatchGetVirtualClusterNodeResourceUsages(
		context.Background(),
		"test",
		"sub-id",
		"cn-pj-01",
		"vc-test",
		"vc-vc-uid",
		[]string{"node-1", "node-2"},
	)
	if err != nil {
		t.Fatalf("BatchGetVirtualClusterNodeResourceUsages() error = %v", err)
	}
	if len(result) != 1 || result[0].UID != "node-1" || result[0].Usage.Available.Device != "4" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
