package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestFindUsersFilteredLookupPreservesMatchingAndSorting(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		users      []IAMUser
		wantIDs    []string
	}{
		{
			name: "Chinese name prefixes and duplicate names", identifier: "周存亮",
			users: []IAMUser{
				{ID: "z", Username: "same", Name: "周存亮"},
				{ID: "a", Username: "same", Name: "周存亮"},
				{ID: "b", Username: "before", Name: "周存亮二"},
				{ID: "contained", Username: "first", Name: "张周存亮"},
				{ID: "unrelated", Username: "other", Name: "他人"},
			},
			wantIDs: []string{"b", "a", "z"},
		},
		{
			name: "Chinese contained names", identifier: "存亮",
			users:   []IAMUser{{ID: "z", Username: "z", Name: "周存亮"}, {ID: "a", Username: "a", Name: "李存亮"}},
			wantIDs: []string{"a", "z"},
		},
		{
			name: "exact username wins over name prefix", identifier: "周存亮",
			users:   []IAMUser{{ID: "prefix", Username: "a", Name: "周存亮"}, {ID: "exact", Username: "周存亮", Name: "别人"}},
			wantIDs: []string{"exact"},
		},
		{
			name: "exact ID wins over name prefix", identifier: "user-id",
			users:   []IAMUser{{ID: "prefix", Username: "a", Name: "user-id extended"}, {ID: "user-id", Username: "z", Name: "other"}},
			wantIDs: []string{"user-id"},
		},
		{
			name: "case insensitive prefixes retain original precedence", identifier: "ALPHA",
			users:   []IAMUser{{ID: "b", Username: "alpha-more"}, {ID: "a", Username: "alpha"}, {ID: "c", Username: "contains-alpha"}},
			wantIDs: []string{"a", "b"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := iamUserLookupTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Host != "other.example.test" {
					t.Fatalf("lookup crossed profiles: host = %q", r.URL.Host)
				}
				switch r.URL.Path {
				case "/iam/idp/v1/getUsers":
					return iamUserLookupResponse(r, http.StatusOK, `{"users":[]}`), nil
				case "/iam/idp/v1/users":
					filter := r.URL.Query().Get("filter")
					for _, field := range []string{"username", "id", "name"} {
						if !strings.Contains(filter, fmt.Sprintf(`%s="*%s*"`, field, test.identifier)) {
							t.Fatalf("missing %s search term in %q", field, filter)
						}
					}
					if r.URL.Query().Get("page_size") != "100" || r.URL.Query().Get("page_token") != "1" {
						t.Fatalf("unexpected pagination: %v", r.URL.Query())
					}
					body, err := json.Marshal(iamUserListResponse{Users: test.users, TotalSize: len(test.users), NextPageToken: "2"})
					if err != nil {
						t.Fatal(err)
					}
					return iamUserLookupResponse(r, http.StatusOK, string(body)), nil
				default:
					t.Fatalf("unexpected request path %q", r.URL.Path)
					return nil, fmt.Errorf("unexpected path")
				}
			}))
			users, err := client.FindUsersForProfile(context.Background(), "other", test.identifier)
			if err != nil {
				t.Fatal(err)
			}
			if requests != 2 {
				t.Fatalf("request count = %d, want exact lookup plus one filtered page", requests)
			}
			gotIDs := make([]string, 0, len(users))
			for _, user := range users {
				gotIDs = append(gotIDs, user.ID)
			}
			if !reflect.DeepEqual(gotIDs, test.wantIDs) {
				t.Fatalf("IDs = %v, want %v", gotIDs, test.wantIDs)
			}
		})
	}
}

func TestFindUsersFilteredLookupEscapesFilterValues(t *testing.T) {
	identifier := `周"\亮`
	requests := 0
	client := iamUserLookupTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/iam/idp/v1/getUsers" {
			return iamUserLookupResponse(r, http.StatusOK, `{"users":[]}`), nil
		}
		wantFilter := `(status="valid") AND (username="*周\"\\亮*" OR id="*周\"\\亮*" OR name="*周\"\\亮*")`
		if filter := r.URL.Query().Get("filter"); filter != wantFilter {
			t.Fatalf("filter = %q, want %q", filter, wantFilter)
		}
		body, err := json.Marshal(iamUserListResponse{Users: []IAMUser{{ID: "found", Name: identifier}}, TotalSize: 1})
		if err != nil {
			t.Fatal(err)
		}
		return iamUserLookupResponse(r, http.StatusOK, string(body)), nil
	}))
	users, err := client.FindUsers(context.Background(), identifier)
	if err != nil || len(users) != 1 || users[0].ID != "found" || requests != 2 {
		t.Fatalf("users = %#v, error = %v, requests = %d", users, err, requests)
	}
}

func TestFindUsersFilteredLookupReadsAllPagesWithoutTrailingRequest(t *testing.T) {
	users := make([]IAMUser, 101)
	for i := range users {
		users[i] = IAMUser{ID: fmt.Sprintf("user-%03d", i), Username: fmt.Sprintf("user-%03d", i), Name: "周存亮"}
	}
	requests := 0
	client := iamUserLookupTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/iam/idp/v1/getUsers" {
			return iamUserLookupResponse(r, http.StatusOK, `{"users":[]}`), nil
		}
		var pageUsers []IAMUser
		var nextToken string
		switch r.URL.Query().Get("page_token") {
		case "1":
			pageUsers, nextToken = users[:100], "2"
		case "2":
			pageUsers, nextToken = users[100:], "3"
		default:
			t.Fatal("requested a trailing page after reaching total_size")
		}
		body, err := json.Marshal(iamUserListResponse{Users: pageUsers, TotalSize: len(users), NextPageToken: nextToken})
		if err != nil {
			t.Fatal(err)
		}
		return iamUserLookupResponse(r, http.StatusOK, string(body)), nil
	}))
	got, err := client.FindUsers(context.Background(), "周存亮")
	if err != nil || len(got) != len(users) || requests != 3 {
		t.Fatalf("users = %d, error = %v, requests = %d; want 101 users in 3 requests", len(got), err, requests)
	}
	if got[0].ID != "user-000" || got[100].ID != "user-100" {
		t.Fatalf("missing or unsorted pages: first = %s, last = %s", got[0].ID, got[100].ID)
	}
}

func TestFindUsersFilteredLookupFallsBackForUnsupportedOrUnmatchedFilter(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "unsupported", status: http.StatusBadRequest, body: `unsupported filter field name`},
		{name: "empty", status: http.StatusOK, body: `{"users":[]}`},
		{name: "nonmatching response", status: http.StatusOK, body: `{"users":[{"id":"other","name":"他人"}],"total_size":1}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			legacyRequests := 0
			client := iamUserLookupTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Path == "/iam/idp/v1/getUsers" {
					return iamUserLookupResponse(r, http.StatusOK, `{"users":[]}`), nil
				}
				if r.URL.Query().Get("filter") == `(status="valid")` {
					legacyRequests++
					return iamUserLookupResponse(r, http.StatusOK, `{"users":[{"id":"found","name":"周存亮"}],"next_page_token":""}`), nil
				}
				return iamUserLookupResponse(r, test.status, test.body), nil
			}))
			users, err := client.FindUsers(context.Background(), "周存亮")
			if err != nil || len(users) != 1 || users[0].ID != "found" || requests != 3 || legacyRequests != 1 {
				t.Fatalf("users = %#v, error = %v, requests = %d, legacy requests = %d", users, err, requests, legacyRequests)
			}
		})
	}
}

func TestFindUsersFilteredLookupFollowsCursorWithoutTotalSize(t *testing.T) {
	requests := 0
	client := iamUserLookupTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/iam/idp/v1/getUsers" {
			return iamUserLookupResponse(r, http.StatusOK, `{"users":[]}`), nil
		}
		switch r.URL.Query().Get("page_token") {
		case "1":
			return iamUserLookupResponse(r, http.StatusOK, `{"users":[{"id":"a","username":"a","name":"周存亮"}],"next_page_token":"cursor-2"}`), nil
		case "cursor-2":
			return iamUserLookupResponse(r, http.StatusOK, `{"users":[{"id":"b","username":"b","name":"周存亮二"}],"next_page_token":""}`), nil
		default:
			t.Fatal("unexpected page token")
			return nil, fmt.Errorf("unexpected page token")
		}
	}))
	users, err := client.FindUsers(context.Background(), "周存亮")
	if err != nil || len(users) != 2 || requests != 3 || users[0].ID != "a" || users[1].ID != "b" {
		t.Fatalf("users = %#v, error = %v, requests = %d", users, err, requests)
	}
}

func TestFindUsersFilteredLookupHandlesExactAPIFailure(t *testing.T) {
	requests := 0
	client := iamUserLookupTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/iam/idp/v1/getUsers" {
			return iamUserLookupResponse(r, http.StatusBadRequest, "exact lookup unavailable"), nil
		}
		return iamUserLookupResponse(r, http.StatusOK, `{"users":[{"id":"found","name":"周存亮"}],"total_size":1}`), nil
	}))
	users, err := client.FindUsers(context.Background(), "周存亮")
	if err != nil || len(users) != 1 || users[0].ID != "found" || requests != 2 {
		t.Fatalf("users = %#v, error = %v, requests = %d", users, err, requests)
	}
}

func TestFindUsersFilteredLookupDoesNotFallBackForForbidden(t *testing.T) {
	requests := 0
	client := iamUserLookupTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/iam/idp/v1/getUsers" {
			return iamUserLookupResponse(r, http.StatusOK, `{"users":[]}`), nil
		}
		if r.URL.Query().Get("filter") == `(status="valid")` {
			t.Fatal("permission failure triggered a full directory scan")
		}
		return iamUserLookupResponse(r, http.StatusForbidden, "server says returned 400: unsupported"), nil
	}))
	_, err := client.FindUsers(context.Background(), "周存亮")
	if err == nil || !strings.Contains(err.Error(), "returned 403:") || requests != 2 {
		t.Fatalf("error = %v, requests = %d", err, requests)
	}
}

func TestFindUsersFilteredLookupDoesNotFallBackAfterCancellation(t *testing.T) {
	for _, cancelExact := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel exact=%v", cancelExact), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			requests := 0
			client := iamUserLookupTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Path == "/iam/idp/v1/getUsers" && !cancelExact {
					return iamUserLookupResponse(r, http.StatusOK, `{"users":[]}`), nil
				}
				if r.URL.Query().Get("filter") == `(status="valid")` {
					t.Fatal("cancellation triggered a full directory scan")
				}
				cancel()
				return nil, context.Canceled
			}))
			_, err := client.FindUsers(ctx, "周存亮")
			wantRequests := 2
			if cancelExact {
				wantRequests = 1
			}
			if !errors.Is(err, context.Canceled) || requests != wantRequests {
				t.Fatalf("error = %v, requests = %d, want cancellation after %d requests", err, requests, wantRequests)
			}
		})
	}
}

func iamUserLookupTestClient(transport http.RoundTripper) *VirtualClusterClient {
	return &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d":     {Name: "d", AccessKey: "ak", SecretKey: "sk", IAMBaseURL: "https://default.example.test"},
			"other": {Name: "other", AccessKey: "other-ak", SecretKey: "other-sk", IAMBaseURL: "https://other.example.test"},
		},
		httpClient: &http.Client{Transport: transport},
	}
}

func iamUserLookupResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}
}
