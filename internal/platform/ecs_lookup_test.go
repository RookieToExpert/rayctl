package platform

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestFindCurrentProfileAISpacesUsesExactFilter(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/compute/ais/v1/subscriptions/-/resourceGroups/-/zones/-/aiSpaces" {
			return nil, fmt.Errorf("unexpected path %q", request.URL.Path)
		}
		wantFilter := `name="ais-test" OR display_name="ais-test"`
		if got := request.URL.Query().Get("filter"); got != wantFilter {
			return nil, fmt.Errorf("filter = %q, want %q", got, wantFilter)
		}
		return jsonHTTPResponse(request, `{"ai_spaces":[{"uid":"ais-uid","name":"ais-test"}],"total_size":1}`), nil
	})}
	client := &VirtualClusterClient{
		accessKey:      "ak",
		secretKey:      "sk",
		baseURL:        "https://example.test",
		currentProfile: "test",
		httpClient:     httpClient,
	}

	items, err := client.FindCurrentProfileAISpaces(context.Background(), "ais-test")
	if err != nil {
		t.Fatalf("FindCurrentProfileAISpaces() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "ais-test" || items[0].ProfileName != "test" {
		t.Fatalf("items = %#v", items)
	}
}

func TestFindCurrentProfileECSVirtualMachinesUsesExactFilter(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/compute/ecs/v2/subscriptions/-/resourceGroups/-/zones/-/virtualMachines" {
			return nil, fmt.Errorf("unexpected path %q", request.URL.Path)
		}
		wantFilter := `name="ecs-test" OR display_name="ecs-test"`
		if got := request.URL.Query().Get("filter"); got != wantFilter {
			return nil, fmt.Errorf("filter = %q, want %q", got, wantFilter)
		}
		return jsonHTTPResponse(request, `{"virtual_machines":[{"uid":"ecs-uid","name":"ecs-test"}],"total_size":1}`), nil
	})}
	client := &VirtualClusterClient{
		accessKey:      "ak",
		secretKey:      "sk",
		baseURL:        "https://example.test",
		currentProfile: "test",
		httpClient:     httpClient,
	}

	items, err := client.FindCurrentProfileECSVirtualMachines(context.Background(), "ecs-test")
	if err != nil {
		t.Fatalf("FindCurrentProfileECSVirtualMachines() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "ecs-test" || items[0].ProfileName != "test" {
		t.Fatalf("items = %#v", items)
	}
}

func TestFindCurrentProfileAISpacesUsesUIDFilter(t *testing.T) {
	const uid = "019fff80-5540-7715-b784-832622a91c72"
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got, want := request.URL.Query().Get("filter"), `uid="`+uid+`"`; got != want {
			return nil, fmt.Errorf("filter = %q, want %q", got, want)
		}
		return jsonHTTPResponse(request, `{"ai_spaces":[{"uid":"`+uid+`","name":"ais-zhu"}],"total_size":1}`), nil
	})}
	client := &VirtualClusterClient{
		accessKey:      "ak",
		secretKey:      "sk",
		baseURL:        "https://example.test",
		currentProfile: "test",
		httpClient:     httpClient,
	}

	items, err := client.FindCurrentProfileAISpaces(context.Background(), uid)
	if err != nil {
		t.Fatalf("FindCurrentProfileAISpaces() error = %v", err)
	}
	if len(items) != 1 || items[0].UID != uid {
		t.Fatalf("items = %#v", items)
	}
}
