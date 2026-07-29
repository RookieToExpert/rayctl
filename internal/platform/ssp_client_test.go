package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type sspRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn sspRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestFindSSPTrainingJobsUsesRegionProfileAndExactFilter(t *testing.T) {
	var requestPath string
	var requestFilter string
	httpClient := &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestPath = r.URL.Path
		requestFilter = r.URL.Query().Get("filter")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "training_jobs": [{
    "uid": "0908d002-fc65-4291-a268-102360023265",
    "name": "demo-job",
    "workspace_name": "ws-demo",
    "status": {"state": "PENDING"},
    "spec": {"volume_mounts": [{"type": "PV_OCEANSTOR", "name": "afs-demo"}]}
  }],
  "total_size": 1
}`)),
			Request: r,
		}, nil
	})}

	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d": {
				Name:              "d",
				AccessKey:         "d-ak",
				SecretKey:         "d-sk",
				KubernetesBaseURL: "https://compute.d.example.com",
				Region:            "cn-pj-01",
			},
			"pt": {
				Name:              "pt",
				AccessKey:         "pt-ak",
				SecretKey:         "pt-sk",
				KubernetesBaseURL: "https://compute.pjlab.org.cn",
				ResourceGroup:     "default",
				Region:            "cn-pj-03",
			},
		},
		httpClient: httpClient,
	}

	jobs, err := client.FindSSPTrainingJobs(context.Background(), "subscription-1", "cn-pj-03", "ws-demo", "demo-job")
	if err != nil {
		t.Fatalf("FindSSPTrainingJobs returned error: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ProfileName != "pt" {
		t.Fatalf("unexpected jobs: %#v", jobs)
	}
	if len(jobs[0].Spec.VolumeMounts) != 1 || jobs[0].Spec.VolumeMounts[0].Name != "afs-demo" {
		t.Fatalf("volume mounts were not decoded: %#v", jobs[0].Spec.VolumeMounts)
	}
	if requestPath != "/ait/data/v1/subscriptions/subscription-1/resourceGroups/default/regions/cn-pj-03/workspaces/ws-demo/trainingJobs" {
		t.Fatalf("unexpected request path: %s", requestPath)
	}
	if requestFilter != `name="demo-job"` {
		t.Fatalf("unexpected filter: %q", requestFilter)
	}
}

func TestConfiguredSubscriptionForRegion(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d":  {Name: "d", Region: "cn-pj-01", Subscription: "d-sub"},
			"pt": {Name: "pt", Region: "cn-pj-03", Subscription: "pt-sub"},
		},
	}
	if got := client.ConfiguredSubscriptionForRegion("cn-pj-03"); got != "pt-sub" {
		t.Fatalf("ConfiguredSubscriptionForRegion() = %q, want pt-sub", got)
	}
}

func TestListSSPWorkspacesUsesRMHRegionFilter(t *testing.T) {
	var requestMethod string
	var requestPath string
	var requestFilter string
	httpClient := &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestMethod = r.Method
		requestPath = r.URL.Path
		requestFilter = r.URL.Query().Get("filter")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "resources": [
    {"name": "ws-t-two", "type": "compute.ssp.v1.workspace", "rid": "/subscriptions/sub-2/resourceGroups/default/regions/cn-pj-03/workspaces/ws-t-two"},
    {"name": "ws-t-one", "type": "compute.ssp.v1.workspace", "rid": "/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-03/workspaces/ws-t-one"},
    {"name": "ignore-me", "type": "compute.ecp.v1.virtualCluster"}
  ]
}`)),
			Request: r,
		}, nil
	})}

	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"pt": {
				Name:      "pt",
				AccessKey: "pt-ak",
				SecretKey: "pt-sk",
				BaseURL:   "https://management.pjlab.org.cn",
				Region:    "cn-pj-03",
			},
		},
		httpClient: httpClient,
	}

	workspaces, err := client.ListSSPWorkspaces(context.Background(), "cn-pj-03")
	if err != nil {
		t.Fatalf("ListSSPWorkspaces returned error: %v", err)
	}
	if len(workspaces) != 2 || workspaces[0].Name != "ws-t-one" || workspaces[0].Subscription != "sub-1" ||
		workspaces[1].Name != "ws-t-two" || workspaces[1].Subscription != "sub-2" {
		t.Fatalf("unexpected workspaces: %#v", workspaces)
	}
	if requestMethod != http.MethodPost || requestPath != "/rmh/v1/resources:page" {
		t.Fatalf("unexpected request: %s %s", requestMethod, requestPath)
	}
	if requestFilter != `resource_type="compute.ssp.v1.workspace" AND region="*cn-pj-03*"` {
		t.Fatalf("unexpected filter: %q", requestFilter)
	}
}
