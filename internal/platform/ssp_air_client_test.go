package platform

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestListSSPAIRJobsUsesWorkspacePathFilterAndProfile(t *testing.T) {
	var path, filter string
	client := testSSPAIRClient(func(request *http.Request) string {
		path = request.URL.Path
		filter = request.URL.Query().Get("filter")
		return `{"airs":[{"name":"infer-demo","uid":"job-uid","status":{"state":"RUNNING"}}],"total_size":1}`
	})
	items, err := client.ListSSPAIRJobs(context.Background(), testSSPAIRWorkspace(), "infer-demo")
	if err != nil {
		t.Fatalf("ListSSPAIRJobs() error = %v", err)
	}
	if path != "/air/data/v1/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-01/workspaces/ws-demo/airs" {
		t.Fatalf("request path = %q", path)
	}
	if filter != `name="infer-demo"` {
		t.Fatalf("filter = %q", filter)
	}
	if len(items) != 1 || items[0].WorkspaceName != "ws-demo" || items[0].Region != "cn-pj-01" || items[0].ProfileName != "d" {
		t.Fatalf("items = %#v", items)
	}
}

func TestListSSPAIRJobsPaginates(t *testing.T) {
	requests := 0
	client := testSSPAIRClient(func(request *http.Request) string {
		requests++
		skip, _ := strconv.Atoi(request.URL.Query().Get("skip"))
		count := 100
		if skip == 100 {
			count = 1
		}
		items := make([]string, count)
		for index := range items {
			items[index] = `{"name":"job-` + strconv.Itoa(skip+index) + `"}`
		}
		return `{"airs":[` + strings.Join(items, ",") + `],"total_size":101}`
	})
	items, err := client.ListSSPAIRJobs(context.Background(), testSSPAIRWorkspace(), "")
	if err != nil {
		t.Fatalf("ListSSPAIRJobs() error = %v", err)
	}
	if requests != 2 || len(items) != 101 || items[100].Name != "job-100" {
		t.Fatalf("requests=%d items=%d last=%#v", requests, len(items), items[len(items)-1])
	}
}

func TestGetSSPAIRJobAndWorkers(t *testing.T) {
	client := testSSPAIRClient(func(request *http.Request) string {
		switch {
		case strings.HasSuffix(request.URL.Path, "/workers"):
			if request.URL.Query().Get("page_size") != "20" {
				t.Fatalf("worker page_size = %q", request.URL.Query().Get("page_size"))
			}
			return `{"workers":[{"name":"infer-demo-0","phase":"RUNNING","host_ip":"10.0.0.1"}],"total_size":990}`
		case strings.HasSuffix(request.URL.Path, "/airs/infer-demo"):
			return `{"name":"infer-demo","status":{"ready_replicas":3,"replicas":4,"leader_service_cluster_ip":"10.1.2.3"}}`
		default:
			t.Fatalf("unexpected path %q", request.URL.Path)
			return "{}"
		}
	})
	job, err := client.GetSSPAIRJob(context.Background(), SSPAIRJob{
		Name: "infer-demo", WorkspaceName: "ws-demo", SubscriptionName: "sub-1", ResourceGroupName: "default", Region: "cn-pj-01", ProfileName: "d",
	})
	if err != nil {
		t.Fatalf("GetSSPAIRJob() error = %v", err)
	}
	workers, total, err := client.ListSSPAIRWorkers(context.Background(), *job, 20)
	if err != nil {
		t.Fatalf("ListSSPAIRWorkers() error = %v", err)
	}
	if job.Status.ReadyReplicas != 3 || len(workers) != 1 || total != 990 || workers[0].HostIP != "10.0.0.1" {
		t.Fatalf("job=%#v workers=%#v total=%d", job, workers, total)
	}
}

func TestListSSPAIRWorkersPaginatesToRequestedLimit(t *testing.T) {
	requests := 0
	client := testSSPAIRClient(func(request *http.Request) string {
		requests++
		skip, _ := strconv.Atoi(request.URL.Query().Get("skip"))
		pageSize, _ := strconv.Atoi(request.URL.Query().Get("page_size"))
		if pageSize > 100 {
			t.Fatalf("worker page_size = %d, want <= 100", pageSize)
		}
		remaining := 250 - skip
		if remaining < pageSize {
			pageSize = remaining
		}
		items := make([]string, pageSize)
		for index := range items {
			items[index] = `{"name":"worker-` + strconv.Itoa(skip+index) + `"}`
		}
		return `{"workers":[` + strings.Join(items, ",") + `],"total_size":250}`
	})
	job := SSPAIRJob{Name: "infer-demo", WorkspaceName: "ws-demo", SubscriptionName: "sub-1", ResourceGroupName: "default", Region: "cn-pj-01", ProfileName: "d"}
	workers, total, err := client.ListSSPAIRWorkers(context.Background(), job, 1000)
	if err != nil {
		t.Fatalf("ListSSPAIRWorkers() error = %v", err)
	}
	if requests != 3 || len(workers) != 250 || total != 250 || workers[249].Name != "worker-249" {
		t.Fatalf("requests=%d workers=%d total=%d last=%#v", requests, len(workers), total, workers[len(workers)-1])
	}
}

func TestListSSPAIRGatewaysParsesFullListObject(t *testing.T) {
	client := testSSPAIRClient(func(request *http.Request) string {
		return `{"infer_gateways":[{"name":"service-demo","uid":"gateway-uid","spec":{"replicas":2,"queue":{"name":"queue-demo"}},"status":{"state":"RUNNING"}}],"total_size":1}`
	})
	items, err := client.ListSSPAIRGateways(context.Background(), testSSPAIRWorkspace(), "service-demo")
	if err != nil {
		t.Fatalf("ListSSPAIRGateways() error = %v", err)
	}
	if len(items) != 1 || items[0].Spec.Replicas != 2 || items[0].Spec.Queue.Name != "queue-demo" || items[0].WorkspaceName != "ws-demo" {
		t.Fatalf("items = %#v", items)
	}
}

func testSSPAIRWorkspace() SSPWorkspace {
	return SSPWorkspace{Name: "ws-demo", Subscription: "sub-1", ResourceGroup: "default", Region: "cn-pj-01", ProfileName: "d"}
}

func testSSPAIRClient(response func(*http.Request) string) *VirtualClusterClient {
	return &VirtualClusterClient{
		profiles: map[string]clientProfile{"d": {
			Name: "d", AccessKey: "ak", SecretKey: "sk", KubernetesBaseURL: "https://compute.d.example.com", ResourceGroup: "default", Region: "cn-pj-01",
		}},
		httpClient: &http.Client{Transport: sspRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(response(request))),
				Request:    request,
			}, nil
		})},
	}
}
