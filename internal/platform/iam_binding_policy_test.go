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
