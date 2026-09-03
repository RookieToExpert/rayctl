package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestJobRequestsForProfileDoNotProbeOtherProfiles(t *testing.T) {
	requestCount := 0
	requestedPaths := make([]string, 0, 9)
	requestedAccepts := make([]string, 0, 9)
	requestedFieldSelectors := make([]string, 0, 10)
	requestedFilters := make([]string, 0, 11)
	requestedPageSizes := make([]string, 0, 11)
	clientHTTP := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		requestedPaths = append(requestedPaths, r.URL.Path)
		requestedAccepts = append(requestedAccepts, r.Header.Get("Accept"))
		requestedFieldSelectors = append(requestedFieldSelectors, r.URL.Query().Get("fieldSelector"))
		requestedFilters = append(requestedFilters, r.URL.Query().Get("filter"))
		requestedPageSizes = append(requestedPageSizes, r.URL.Query().Get("page_size"))
		if r.URL.Host != "pt-compute.example.test" {
			t.Fatalf("request host = %q, want pt-compute.example.test", r.URL.Host)
		}

		body := `{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","metadata":{"name":"example-job","namespace":"default"}}`
		if strings.HasSuffix(r.URL.Path, "/pods") {
			body = `{"apiVersion":"v1","kind":"PodList","items":[]}`
		} else if strings.HasSuffix(r.URL.Path, "/jobs") {
			body = `{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"JobList","items":[]}`
			if r.URL.Query().Get("page_size") == "1" {
				body = `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"remainingItemCount":9547},"items":[{"metadata":{"name":"example-job"}}]}`
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d": {
				Name:              "d",
				AccessKey:         "d-ak",
				SecretKey:         "d-sk",
				KubernetesBaseURL: "https://d-compute.example.test",
			},
			"pt": {
				Name:              "pt",
				AccessKey:         "pt-ak",
				SecretKey:         "pt-sk",
				KubernetesBaseURL: "https://pt-compute.example.test",
			},
		},
		httpClient: clientHTTP,
	}

	if _, err := client.GetVolcanoJobForProfile(context.Background(), "pt", "vc-example", "default", "example-job"); err != nil {
		t.Fatalf("GetVolcanoJobForProfile() error = %v", err)
	}
	if _, err := client.ListJobPodsForProfile(context.Background(), "pt", "vc-example", "default", "example-job"); err != nil {
		t.Fatalf("ListJobPodsForProfile() error = %v", err)
	}
	if _, err := client.ListVolcanoJobsForProfile(context.Background(), "pt", "vc-example", "default"); err != nil {
		t.Fatalf("ListVolcanoJobsForProfile() error = %v", err)
	}
	if _, err := client.ListPodsForProfile(context.Background(), "pt", "vc-example", "default"); err != nil {
		t.Fatalf("ListPodsForProfile() error = %v", err)
	}
	if _, err := client.ListActivePodsForProfile(context.Background(), "pt", "vc-example", "default"); err != nil {
		t.Fatalf("ListActivePodsForProfile() error = %v", err)
	}
	if _, err := client.GetSecretForProfile(context.Background(), "pt", "vc-example", "default", "pull-secret"); err != nil {
		t.Fatalf("GetSecretForProfile() error = %v", err)
	}
	if _, err := client.GetPersistentVolumeClaimForProfile(context.Background(), "pt", "vc-example", "default", "data-pvc"); err != nil {
		t.Fatalf("GetPersistentVolumeClaimForProfile() error = %v", err)
	}
	if _, err := client.GetPersistentVolumeForProfile(context.Background(), "pt", "vc-example", "data-pv"); err != nil {
		t.Fatalf("GetPersistentVolumeForProfile() error = %v", err)
	}
	if _, err := client.ListVolcanoJobMetadataForProfile(context.Background(), "pt", "vc-example", "default"); err != nil {
		t.Fatalf("ListVolcanoJobMetadataForProfile() error = %v", err)
	}
	count, err := client.CountVolcanoJobsForProfile(context.Background(), "pt", "vc-example", "default")
	if err != nil {
		t.Fatalf("CountVolcanoJobsForProfile() error = %v", err)
	}
	if count != 9548 {
		t.Fatalf("CountVolcanoJobsForProfile() = %d, want 9548", count)
	}
	if _, err := client.ListVolcanoJobsPageForProfile(context.Background(), "pt", "vc-example", `(state="Running" OR state="Pending")`, 5); err != nil {
		t.Fatalf("ListVolcanoJobsPageForProfile() error = %v", err)
	}
	if requestCount != 11 {
		t.Fatalf("request count = %d, want 11", requestCount)
	}
	if got := requestedPaths[3]; !strings.HasSuffix(got, "/api/v1/namespaces/default/pods") {
		t.Fatalf("cluster pod list path = %q, want namespace-scoped pod path", got)
	}
	if got := requestedFieldSelectors[4]; got != "status.phase!=Succeeded,status.phase!=Failed" {
		t.Fatalf("active pod fieldSelector = %q", got)
	}
	if got := requestedAccepts[8]; !strings.Contains(got, "PartialObjectMetadataList") {
		t.Fatalf("job metadata Accept = %q, want PartialObjectMetadataList", got)
	}
	if got := requestedAccepts[9]; !strings.Contains(got, "PartialObjectMetadataList") {
		t.Fatalf("job count Accept = %q, want PartialObjectMetadataList", got)
	}
	if got := requestedFilters[10]; got != `(state="Running" OR state="Pending")` {
		t.Fatalf("paged job filter = %q", got)
	}
	if got := requestedPageSizes[10]; got != "5" {
		t.Fatalf("paged job page_size = %q, want 5", got)
	}
}
