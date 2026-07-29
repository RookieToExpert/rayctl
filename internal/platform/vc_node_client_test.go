package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListVirtualClusterNodesPaginatesBySkip(t *testing.T) {
	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path != "/compute/ecp/v1/subscriptions/sub-id/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-test/nodePools/-/nodes" {
			return nil, fmt.Errorf("unexpected path %s", r.URL.Path)
		}
		body := ""
		switch r.URL.Query().Get("skip") {
		case "0":
			body = `{"ai_compute_nodes":[{"uid":"uid-1","properties":{"host_ip":"10.0.0.1"}}],"bare_metal_nodes":[{"uid":"uid-2","properties":{"host_ip":"10.0.0.2"}}],"total_size":3,"next_page_token":"next"}`
		case "2":
			body = `{"ai_compute_nodes":[{"uid":"uid-3","properties":{"host_ip":"10.0.0.3"}}],"total_size":3}`
		default:
			return nil, fmt.Errorf("unexpected skip %q", r.URL.Query().Get("skip"))
		}
		return jsonHTTPResponse(r, body), nil
	})}
	client := &VirtualClusterClient{
		accessKey:     "ak",
		secretKey:     "sk",
		baseURL:       "https://example.test",
		resourceGroup: "default",
		region:        "cn-pj-01",
		httpClient:    httpClient,
	}

	nodes, err := client.ListVirtualClusterNodes(context.Background(), "", "sub-id", "cn-pj-01", "vc-test")
	if err != nil {
		t.Fatalf("ListVirtualClusterNodes() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(nodes))
	}
	if nodes[1].Kind != "BMS" || nodes[2].UID != "uid-3" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
}

func TestRemoveAIComputeNodesFromVirtualClusterPayload(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			return nil, fmt.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/compute/ecp/v1/subscriptions/sub-id/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-test/AIComputeNodes:remove" {
			return nil, fmt.Errorf("unexpected path %s", r.URL.Path)
		}
		var payload struct {
			SubscriptionName   string   `json:"subscription_name"`
			ResourceGroupName  string   `json:"resource_group_name"`
			Region             string   `json:"region"`
			VirtualClusterName string   `json:"virtual_cluster_name"`
			ACNUIDs            []string `json:"acn_uids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, err
		}
		if payload.SubscriptionName != "sub-id" || payload.ResourceGroupName != "default" || payload.VirtualClusterName != "vc-test" {
			t.Errorf("unexpected payload: %#v", payload)
		}
		if len(payload.ACNUIDs) != 2 || payload.ACNUIDs[1] != "uid-2" {
			t.Errorf("acn_uids = %#v", payload.ACNUIDs)
		}
		return jsonHTTPResponse(r, `{"ai_compute_nodes":[{"uid":"uid-1"},{"uid":"uid-2"}]}`), nil
	})}
	client := &VirtualClusterClient{
		accessKey:     "ak",
		secretKey:     "sk",
		baseURL:       "https://example.test",
		resourceGroup: "default",
		region:        "cn-pj-01",
		httpClient:    httpClient,
	}

	removed, err := client.RemoveAIComputeNodesFromVirtualCluster(
		context.Background(), "", "sub-id", "cn-pj-01", "vc-test", []string{"uid-1", "uid-2"},
	)
	if err != nil {
		t.Fatalf("RemoveAIComputeNodesFromVirtualCluster() error = %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed = %d, want 2", len(removed))
	}
}

func jsonHTTPResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
