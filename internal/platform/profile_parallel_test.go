package platform

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestListAIComputeNodesQueriesProfilesConcurrentlyAndMergesInOrder(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	client := &VirtualClusterClient{
		currentProfile: "first",
		profiles: map[string]clientProfile{
			"first":  {Name: "first", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://first.example.test"},
			"second": {Name: "second", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://second.example.test"},
		},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			started <- req.URL.Host
			select {
			case <-release:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
			body := fmt.Sprintf(`{"ai_compute_nodes":[{"uid":"same","name":"%s"}]}`, req.URL.Host)
			return jsonHTTPResponse(req, body), nil
		})},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	type result struct {
		items []AIComputeNode
		err   error
	}
	done := make(chan result, 1)
	go func() {
		items, err := client.ListAIComputeNodes(ctx)
		done <- result{items, err}
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("profiles were not queried concurrently")
		}
	}
	close(release)
	got := <-done
	if got.err != nil || len(got.items) != 1 || got.items[0].ProfileName != "first" {
		t.Fatalf("ListAIComputeNodes() = %#v, %v", got.items, got.err)
	}
}
