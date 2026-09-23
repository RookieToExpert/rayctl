package platform

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestListUsersStopsOnTokenCycle(t *testing.T) {
	next := map[string]string{"1": "A", "A": "B", "B": "A"}
	requests := 0
	client := &VirtualClusterClient{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if requests > 3 {
				return nil, fmt.Errorf("pagination loop")
			}
			token := req.URL.Query().Get("page_token")
			body := fmt.Sprintf(`{"users":[{"id":"%s"}],"next_page_token":"%s"}`, token, next[token])
			return jsonHTTPResponse(req, body), nil
		})},
	}
	items, err := client.listUsersForProfile(context.Background(), clientProfile{Name: "test", AccessKey: "ak", SecretKey: "sk", IAMBaseURL: "https://iam.example.test"})
	if err != nil || requests != 3 || len(items) != 3 {
		t.Fatalf("items = %d, requests = %d, err = %v", len(items), requests, err)
	}
}
