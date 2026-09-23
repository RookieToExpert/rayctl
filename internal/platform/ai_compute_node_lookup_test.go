package platform

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestFindAIComputeNodesUsesExactFilter(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/compute/ecp/v1/subscriptions/-/resourceGroups/-/zones/-/aiComputeNodes" {
			return nil, fmt.Errorf("unexpected path %q", request.URL.Path)
		}
		wantFilter := `name="ecp-c550-h3c-62b1" OR display_name="ecp-c550-h3c-62b1"`
		if got := request.URL.Query().Get("filter"); got != wantFilter {
			return nil, fmt.Errorf("filter = %q, want %q", got, wantFilter)
		}
		return jsonHTTPResponse(request, `{"ai_compute_nodes":[{"uid":"node-uid","name":"ecp-c550-h3c-62b1","properties":{"host_name":"host-10-12-34-56"}}],"total_size":1}`), nil
	})}
	client := &VirtualClusterClient{
		accessKey:      "ak",
		secretKey:      "sk",
		baseURL:        "https://example.test",
		currentProfile: "test",
		httpClient:     httpClient,
	}

	items, err := client.FindAIComputeNodes(context.Background(), "ecp-c550-h3c-62b1")
	if err != nil {
		t.Fatalf("FindAIComputeNodes() error = %v", err)
	}
	if len(items) != 1 || items[0].Properties.HostName != "host-10-12-34-56" || items[0].ProfileName != "test" {
		t.Fatalf("items = %#v", items)
	}
}
