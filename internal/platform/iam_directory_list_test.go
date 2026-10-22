package platform

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListIAMUsersForProfileLimitPaginatesWithoutExtraRequests(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/iam/idp/v1/users" || r.URL.Query().Has("filter") {
			t.Errorf("unexpected user request %s", r.URL.String())
		}
		switch r.URL.Query().Get("page_token") {
		case "1":
			if got := r.URL.Query().Get("page_size"); got != "3" {
				t.Errorf("first page size = %q", got)
			}
			fmt.Fprint(w, `{"users":[{"id":"u1","username":"one"},{"id":"u2","username":"two"}],"next_page_token":"2"}`)
		case "2":
			if got := r.URL.Query().Get("page_size"); got != "3" {
				t.Errorf("second page size = %q", got)
			}
			fmt.Fprint(w, `{"users":[{"id":"u3","username":"three"}],"next_page_token":"3"}`)
		default:
			t.Errorf("unexpected page token %q", r.URL.Query().Get("page_token"))
		}
	}))
	defer server.Close()
	client := &VirtualClusterClient{currentProfile: "test", profiles: map[string]clientProfile{"test": {Name: "test", IAMBaseURL: server.URL}}, httpClient: server.Client()}
	users, err := client.ListIAMUsersForProfile(context.Background(), "test", 3)
	if err != nil || len(users) != 3 || users[2].Username != "three" || requests != 2 {
		t.Fatalf("users = %#v, requests = %d, err = %v", users, requests, err)
	}
}

func TestListIAMGroupsForProfileReadsAllPages(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/iam/idp/v1/groups" {
			t.Errorf("unexpected group request %s", r.URL.String())
		}
		switch r.URL.Query().Get("page_token") {
		case "1":
			fmt.Fprint(w, `{"groups":[{"id":"g1","name":"first","tenant_id":"tenant-1"}],"next_page_token":"2"}`)
		case "2":
			fmt.Fprint(w, `{"groups":[{"id":"g2","name":"second"}]}`)
		default:
			t.Errorf("unexpected page token %q", r.URL.Query().Get("page_token"))
		}
	}))
	defer server.Close()
	client := &VirtualClusterClient{currentProfile: "test", profiles: map[string]clientProfile{"test": {Name: "test", IAMBaseURL: server.URL}}, httpClient: server.Client()}
	groups, err := client.ListIAMGroupsForProfile(context.Background(), "test", 0)
	if err != nil || len(groups) != 2 || groups[0].TenantID != "tenant-1" || requests != 2 {
		t.Fatalf("groups = %#v, requests = %d, err = %v", groups, requests, err)
	}
}
