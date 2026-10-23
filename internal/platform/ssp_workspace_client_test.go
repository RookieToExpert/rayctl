package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func workspaceInventoryClient(roundTrip sspRoundTripFunc, multipleProfiles bool) *VirtualClusterClient {
	profiles := map[string]clientProfile{
		"one": {Name: "one", AccessKey: "one-ak", SecretKey: "sk", BaseURL: "https://workspace.test", Region: "cn-pj-01"},
	}
	if multipleProfiles {
		profiles["two"] = clientProfile{Name: "two", AccessKey: "two-ak", SecretKey: "sk", BaseURL: "https://workspace.test", Region: "cn-pj-01"}
	}
	return &VirtualClusterClient{currentProfile: "one", profiles: profiles, httpClient: &http.Client{Transport: roundTrip}}
}

func workspaceInventoryResponse(r *http.Request, start, count, total int, next string) *http.Response {
	resources := []map[string]string{}
	for n := start; n < start+count; n++ {
		name := fmt.Sprintf("ws-%03d", n)
		resources = append(resources, map[string]string{"name": name, "type": "compute.ssp.v1.workspace", "rid": "/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/" + name})
	}
	raw, _ := json.Marshal(map[string]any{"resources": resources, "total_size": total, "next_page_token": next})
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}
}

func TestWorkspaceInventoryRetainsProfileFailures(t *testing.T) {
	client := workspaceInventoryClient(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.Header.Get("Authorization"), "two-ak") {
			return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("PermissionDenied")), Request: r}, nil
		}
		return workspaceInventoryResponse(r, 0, 1, 1, ""), nil
	}, true)
	items, err := client.ListSSPWorkspacesWithStatus(context.Background(), "cn-pj-01")
	var status *HTTPStatusError
	if len(items) != 1 || !errors.As(err, &status) || status.StatusCode != 403 || !strings.Contains(err.Error(), "two") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("items=%v err=%v", items, err)
	}
	// Other commands keep the original best-effort inventory contract.
	items, err = client.ListSSPWorkspaces(context.Background(), "cn-pj-01")
	if len(items) != 1 || err != nil {
		t.Fatalf("legacy inventory items=%v err=%v", items, err)
	}
}

func TestWorkspaceInventoryFollowsShortPagesAndLargeInventories(t *testing.T) {
	for _, firstPage := range []int{1, 200} {
		t.Run(fmt.Sprint(firstPage), func(t *testing.T) {
			pages := []string{}
			client := workspaceInventoryClient(func(r *http.Request) (*http.Response, error) {
				token := r.URL.Query().Get("page_token")
				pages = append(pages, token)
				if token == "1" {
					return workspaceInventoryResponse(r, 0, firstPage, firstPage+1, "2"), nil
				}
				return workspaceInventoryResponse(r, firstPage, 1, firstPage+1, ""), nil
			}, false)
			items, err := client.ListSSPWorkspacesWithStatus(context.Background(), "cn-pj-01")
			if err != nil || len(items) != firstPage+1 || strings.Join(pages, ",") != "1,2" {
				t.Fatalf("items=%d pages=%v err=%v", len(items), pages, err)
			}
		})
	}
}

func TestWorkspaceInventoryIncompletePaginationIsVisible(t *testing.T) {
	for _, next := range []string{"", "1"} {
		client := workspaceInventoryClient(func(r *http.Request) (*http.Response, error) {
			return workspaceInventoryResponse(r, 0, 1, 2, next), nil
		}, false)
		items, err := client.ListSSPWorkspacesWithStatus(context.Background(), "cn-pj-01")
		if len(items) != 1 || err == nil || !strings.Contains(err.Error(), "分页未完成") {
			t.Fatalf("items=%v err=%v", items, err)
		}
	}
}
