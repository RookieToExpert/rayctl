package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListIAMBindingPoliciesForProfileUsesRequestedProfile(t *testing.T) {
	clientHTTP := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "cloud.example.test" {
			t.Fatalf("request host = %q, want cloud.example.test", r.URL.Host)
		}
		if r.URL.Path != "/iam/authz/v1/bindingPolicies" {
			t.Fatalf("request path = %q, want bindingPolicies", r.URL.Path)
		}
		if filter := r.URL.Query().Get("filter"); !strings.Contains(filter, `scope="*ccr-sandbox*"`) {
			t.Fatalf("filter = %q, want ccr-sandbox scope filter", filter)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"policies":[{"id":"policy-cloud","scope":"/rm/subscriptions/cloud/namespaces/ccr-sandbox"}],"next_page_token":""}`,
			)),
			Request: r,
		}, nil
	})}

	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d": {
				Name:       "d",
				AccessKey:  "d-ak",
				SecretKey:  "d-sk",
				IAMBaseURL: "https://d.example.test",
			},
			"dcloud": {
				Name:       "dcloud",
				AccessKey:  "cloud-ak",
				SecretKey:  "cloud-sk",
				IAMBaseURL: "https://cloud.example.test",
			},
		},
		httpClient: clientHTTP,
	}

	policies, err := client.ListIAMBindingPoliciesForResourceProfile(context.Background(), "dcloud", "ccr-sandbox")
	if err != nil {
		t.Fatalf("ListIAMBindingPoliciesForProfile() error = %v", err)
	}
	if len(policies) != 1 || policies[0].ID != "policy-cloud" {
		t.Fatalf("ListIAMBindingPoliciesForProfile() = %#v, want cloud policy", policies)
	}
}

func TestListGroupUsersForProfileUsesGroupMembersEndpoint(t *testing.T) {
	clientHTTP := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/iam/idp/v1/groups/group-uid:getUsers" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("order_by") != "join_time desc" {
			t.Fatalf("order_by = %q", r.URL.Query().Get("order_by"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"users":[{"id":"user-uid","username":"tester","name":"测试用户","join_time":"2026-09-20T01:02:03Z"}],"total_size":1,"next_page_token":"2"}`,
			)),
			Request: r,
		}, nil
	})}
	client := &VirtualClusterClient{
		profiles: map[string]clientProfile{"d": {
			Name: "d", AccessKey: "ak", SecretKey: "sk", IAMBaseURL: "https://d.example.test",
		}},
		httpClient: clientHTTP,
	}

	users, err := client.ListGroupUsersForProfile(context.Background(), "d", "group-uid")
	if err != nil {
		t.Fatalf("ListGroupUsersForProfile() error = %v", err)
	}
	if len(users) != 1 || users[0].Username != "tester" || users[0].JoinTime != "2026-09-20T01:02:03Z" {
		t.Fatalf("users = %#v", users)
	}
}

func TestListIAMBindingPoliciesForSubjectsProfileUsesFilteredLookup(t *testing.T) {
	var filter string
	clientHTTP := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		filter = r.URL.Query().Get("filter")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"policies":[{"id":"policy-1"}]}`)),
			Request:    r,
		}, nil
	})}
	client := &VirtualClusterClient{
		profiles: map[string]clientProfile{"d": {
			Name: "d", AccessKey: "ak", SecretKey: "sk", IAMBaseURL: "https://d.example.test",
		}},
		httpClient: clientHTTP,
	}

	policies, err := client.ListIAMBindingPoliciesForSubjectsProfile(context.Background(), "d", []string{"linhonglin", "group-uid", "linhonglin"})
	if err != nil {
		t.Fatalf("ListIAMBindingPoliciesForSubjectsProfile() error = %v", err)
	}
	for _, fragment := range []string{`member_identify="*linhonglin*"`, `scope="*group-uid*"`} {
		if !strings.Contains(filter, fragment) {
			t.Fatalf("filter = %q, missing %q", filter, fragment)
		}
	}
	if len(policies) != 1 || policies[0].ID != "policy-1" {
		t.Fatalf("policies = %#v", policies)
	}
}
