package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"testing"
)

func TestECSInventoryListsEveryPageOnlyInCurrentProfile(t *testing.T) {
	for _, resourceType := range []string{"ecs", "ais"} {
		t.Run(resourceType, func(t *testing.T) {
			var skips []int
			client := &VirtualClusterClient{
				currentProfile: "current",
				profiles: map[string]clientProfile{
					"current": {Name: "current", BaseURL: "https://current.test", AccessKey: "ak", SecretKey: "sk"},
					"other":   {Name: "other", BaseURL: "https://other.test", AccessKey: "other-ak", SecretKey: "other-sk"},
				},
				httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					path, responseKey := "/compute/ecs/v2/subscriptions/-/resourceGroups/-/zones/-/virtualMachines", "virtual_machines"
					if resourceType == "ais" {
						path, responseKey = "/compute/ais/v1/subscriptions/-/resourceGroups/-/zones/-/aiSpaces", "ai_spaces"
					}
					if request.URL.Host != "current.test" || request.URL.Path != path {
						return nil, fmt.Errorf("unexpected inventory request: %s", request.URL)
					}
					query := request.URL.Query()
					if query.Get("page_size") != "100" || query.Get("filter") != "" {
						return nil, fmt.Errorf("unexpected pagination/filter: %s", request.URL)
					}
					skip, err := strconv.Atoi(query.Get("skip"))
					if err != nil || skip > 200 {
						return nil, fmt.Errorf("unexpected skip %q", query.Get("skip"))
					}
					skips = append(skips, skip)
					items := make([]map[string]string, 0)
					for index := skip; index < min(skip+100, 201); index++ {
						items = append(items, map[string]string{"uid": fmt.Sprintf("uid-%03d", index), "name": fmt.Sprintf("vm-%03d", index)})
					}
					body, _ := json.Marshal(map[string]any{responseKey: items})
					return jsonHTTPResponse(request, string(body)), nil
				})},
			}
			var count int
			if resourceType == "ecs" {
				items, err := client.ListCurrentProfileECSVirtualMachines(context.Background())
				if err != nil || len(items) != 201 {
					t.Fatalf("ListCurrentProfileECSVirtualMachines() count=%d error=%v", len(items), err)
				}
				count = len(items)
				if items[200].UID != "uid-200" || items[200].ProfileName != "current" {
					t.Fatalf("last resource = %#v", items[200])
				}
			} else {
				items, err := client.ListCurrentProfileAISpaces(context.Background())
				if err != nil || len(items) != 201 {
					t.Fatalf("ListCurrentProfileAISpaces() count=%d error=%v", len(items), err)
				}
				count = len(items)
				if items[200].UID != "uid-200" || items[200].ProfileName != "current" {
					t.Fatalf("last resource = %#v", items[200])
				}
			}
			if count != 201 || !reflect.DeepEqual(skips, []int{0, 100, 200}) {
				t.Fatalf("count=%d skips=%v", count, skips)
			}
		})
	}
}

func TestECSCreatorLookupOnlyUsesCurrentProfile(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "current",
		profiles: map[string]clientProfile{
			"current": {Name: "current", IAMBaseURL: "https://current-iam.test", AccessKey: "ak", SecretKey: "sk"},
			"other":   {Name: "other", IAMBaseURL: "https://other-iam.test", AccessKey: "other-ak", SecretKey: "other-sk"},
		},
		httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host != "current-iam.test" || request.URL.Path != "/iam/idp/v1/getUsers" || request.URL.Query().Get("filter") != `id="creator-a"` {
				return nil, fmt.Errorf("unexpected creator lookup: %s", request.URL)
			}
			return jsonHTTPResponse(request, `{"users":[{"id":"creator-a","username":"alice"}]}`), nil
		})},
	}
	users, err := client.ResolveCurrentProfileUsernames(context.Background(), []string{"creator-a"})
	if err != nil || users["creator-a"] != "alice" {
		t.Fatalf("creator usernames = %v, error = %v", users, err)
	}
}
