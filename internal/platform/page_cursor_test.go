package platform

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
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

func TestPageCursorWarnsAtLimit(t *testing.T) {
	cursor := newPageCursor("start")
	for i := 1; i < maxPageCursorTokens; i++ {
		cursor.seen[fmt.Sprint(i)] = struct{}{}
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = oldStderr })
	if cursor.advance("start") || cursor.advance("next") {
		t.Fatal("cursor advanced past page limit")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	message := make([]byte, 256)
	n, err := reader.Read(message)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	if got := string(message[:n]); !strings.Contains(got, "10000") || strings.Count(got, "Warning:") != 1 {
		t.Fatalf("warning = %q", got)
	}
}
