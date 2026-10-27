package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestQueueQuotaUsesDetailAndOwningProfile(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d":  {Name: "d", BaseURL: "https://d.test", AccessKey: "d-ak", SecretKey: "sk", Subscription: "d-sub", Region: "cn-pj-01"},
			"pt": {Name: "pt", BaseURL: "https://pt.test", AccessKey: "pt-ak", SecretKey: "sk", Subscription: "pt-sub", Region: "cn-pj-03"},
		},
		httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != "GET" || r.URL.Host != "pt.test" || r.URL.Path != "/compute/ssp/v1/subscriptions/explicit-sub/resourceGroups/custom/regions/cn-pj-03/clusters/cluster-demo/queues/queue-demo" || !strings.Contains(r.Header.Get("Authorization"), `accesskey="pt-ak"`) {
				t.Errorf("wrong queue quota request: %s %s", r.Method, r.URL)
			}
			// node_status is zero for ELASTIC, but the resource page uses quota.
			return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"properties":{"node_status":{"total":0},"quota":[{"resource_type":"NODE","unit":"node","total":"209","allocated":"172","unallocated":"37"},{"resource_type":"CPU","unit":"core","total":"23408","allocated":"23280","unallocated":"128"}],"machine_type_allocation":[{"machine_type":"x2ls.ri.i80","cluster_capacity":{"total":314},"queue_quota":{"node_max_num":209,"node_allocated":172,"resource_usage":[{"resource_type":"CPU","unit":"core","total":"23408","allocated":"23280","unallocated":"128"}]}}]}}`))}, nil
		})},
	}
	quota, err := client.GetSSPQueueQuota(context.Background(), "pt", "explicit-sub", "custom", "", "cluster-demo", "queue-demo")
	if err != nil || len(quota.Resources) != 2 || quota.Resources[0].Allocated != "172" || quota.Resources[1].Unallocated != "128" || len(quota.MachineTypes) != 1 {
		t.Fatalf("quota=%+v err=%v", quota, err)
	}
	node := quota.MachineTypes[0].Resources[0]
	if node.ResourceType != "NODE" || node.Total != "209" || node.Allocated != "172" || node.Unallocated != "37" {
		t.Fatal(node)
	}
}

func TestQueueQuotaMissingAndZeroValuesAreDistinct(t *testing.T) {
	for _, test := range []struct {
		name, payload string
		wantError     bool
		wantNodes     int
	}{
		{"missing", `{"properties":{"node_status":{"total":0}}}`, true, 0},
		{"explicit zero", `{"properties":{"quota":[{"resource_type":"CPU","unit":"core","total":"0","allocated":"0","unallocated":"0"}]}}`, false, 0},
		{"missing node counts", `{"properties":{"machine_type_allocation":[{"machine_type":"cpu","queue_quota":{"resource_usage":[{"resource_type":"CPU","total":"8","allocated":"8","unallocated":"0"}]}}]}}`, false, 0},
		{"zero node counts", `{"properties":{"machine_type_allocation":[{"machine_type":"cpu","queue_quota":{"node_max_num":0,"node_allocated":0}}]}}`, false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &VirtualClusterClient{profiles: map[string]clientProfile{"test": {Name: "test", BaseURL: "https://test", Subscription: "sub", Region: "region"}}, httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(test.payload)), Request: r}, nil
			})}}
			quota, err := client.GetSSPQueueQuota(context.Background(), "test", "", "", "", "cluster", "queue")
			if (err != nil) != test.wantError {
				t.Fatalf("quota=%+v err=%v", quota, err)
			}
			if quota != nil {
				nodes := 0
				for _, machine := range quota.MachineTypes {
					for _, resource := range machine.Resources {
						if resource.ResourceType == "NODE" {
							nodes++
						}
					}
				}
				if nodes != test.wantNodes {
					t.Fatalf("made up node quota: %+v", quota)
				}
			}
		})
	}
}

func TestQueueQuotaRetainsHTTPFailure(t *testing.T) {
	client := &VirtualClusterClient{profiles: map[string]clientProfile{"test": {Name: "test", BaseURL: "https://test", Subscription: "sub", Region: "region"}}, httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("denied")), Request: r}, nil
	})}}
	quota, err := client.GetSSPQueueQuota(context.Background(), "test", "", "", "", "cluster", "queue")
	var status *HTTPStatusError
	if quota != nil || !errors.As(err, &status) || status.StatusCode != 403 {
		t.Fatalf("quota=%+v err=%v", quota, err)
	}
}
