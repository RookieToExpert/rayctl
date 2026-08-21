package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFindUsersEnrichesSparseExactResult(t *testing.T) {
	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		var body string
		switch request.URL.Path {
		case "/iam/idp/v1/getUsers":
			body = `{"users":[{"id":"user-id","username":"qa-llm-cicd","name":"qa-llm-cicd","status":"valid"}]}`
		case "/iam/idp/v1/users":
			filter := request.URL.Query().Get("filter")
			if filter != `(status="valid") AND (id="user-id")` {
				t.Fatalf("detail filter = %q", filter)
			}
			body = `{"users":[{"id":"user-id","username":"qa-llm-cicd","name":"qa-llm-cicd","tenant_code":"ailabdev","source":"ad","status":"valid"}]}`
		default:
			t.Fatalf("unexpected request path %q", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}

	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d": {Name: "d", AccessKey: "ak", SecretKey: "sk", IAMBaseURL: "https://iam.example.test"},
		},
		httpClient: httpClient,
	}
	users, err := client.FindUsers(context.Background(), "user-id")
	if err != nil {
		t.Fatalf("FindUsers() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("request count = %d, want 2", requests)
	}
	if len(users) != 1 || users[0].TenantCode != "ailabdev" || users[0].Source != "ad" {
		t.Fatalf("FindUsers() = %#v", users)
	}
}

func TestFindUsersDoesNotEnrichCompleteExactResult(t *testing.T) {
	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"users":[{"id":"user-id","username":"user","tenant_code":"ailabdev","source":"ad","status":"valid"}]}`,
			)),
			Request: request,
		}, nil
	})}
	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d": {Name: "d", AccessKey: "ak", SecretKey: "sk", IAMBaseURL: "https://iam.example.test"},
		},
		httpClient: httpClient,
	}
	if _, err := client.FindUsers(context.Background(), "user-id"); err != nil {
		t.Fatalf("FindUsers() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("request count = %d, want 1", requests)
	}
}
