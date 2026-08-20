package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
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

func TestListSSPQueuesUsesWorkspacePathAndNormalizesFields(t *testing.T) {
	var requestPath string
	var pageSize string
	var skip string
	httpClient := &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestPath = r.URL.Path
		pageSize = r.URL.Query().Get("page_size")
		skip = r.URL.Query().Get("skip")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "queues": [{
    "id": "/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-03/workspaces/ws-demo/queues/019fa185-5356-7dcc-9b38-2b7d77f3dcb2",
    "name": "queue-demo",
    "state": "RUNNING",
    "spec": {"queue_type": "RESERVED", "cluster_name": "vc-demo"}
  }],
  "total_size": 1
}`)),
			Request: r,
		}, nil
	})}
	client := &VirtualClusterClient{
		profiles: map[string]clientProfile{
			"pt": {
				Name:          "pt",
				AccessKey:     "ak",
				SecretKey:     "sk",
				BaseURL:       "https://management.pjlab.org.cn",
				ResourceGroup: "default",
				Region:        "cn-pj-03",
			},
		},
		httpClient: httpClient,
	}
	queues, err := client.ListSSPQueues(context.Background(), SSPWorkspace{
		Name: "ws-demo", Subscription: "sub-1", ResourceGroup: "default", Region: "cn-pj-03", ProfileName: "pt",
	})
	if err != nil {
		t.Fatalf("ListSSPQueues returned error: %v", err)
	}
	if requestPath != "/compute/ssp/v1/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-03/workspaces/ws-demo/queues" || pageSize != "100" || skip != "0" {
		t.Fatalf("unexpected request: path=%q page_size=%q skip=%q", requestPath, pageSize, skip)
	}
	if len(queues) != 1 || queues[0].UID != "019fa185-5356-7dcc-9b38-2b7d77f3dcb2" || queues[0].Type != "RESERVED" || queues[0].WorkspaceName != "ws-demo" {
		t.Fatalf("unexpected queues: %#v", queues)
	}
}

func TestGetSSPQueueReadsSchedulingSettings(t *testing.T) {
	var requestPath string
	httpClient := &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestPath = r.URL.Path
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "uid":"queue-uid",
  "name":"queue-demo",
  "state":"RUNNING",
  "properties":{
    "type":"EXCLUSIVE",
    "workspace":{"name":"ws-demo","uid":"ws-uid"},
    "advanced_settings":{"provide_spot_resource_enabled":true,"dequeue_strategy":"BALANCED"}
  }
}`)),
			Request: r,
		}, nil
	})}
	client := &VirtualClusterClient{
		profiles:   map[string]clientProfile{"d": {Name: "d", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://management.d.example.com"}},
		httpClient: httpClient,
	}
	queue, err := client.GetSSPQueue(context.Background(), "d", "sub-1", "default", "cn-pj-01", "cluster-a3", "queue-demo")
	if err != nil {
		t.Fatalf("GetSSPQueue() error = %v", err)
	}
	if requestPath != "/compute/ssp/v1/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a3/queues/queue-demo" {
		t.Fatalf("request path = %q", requestPath)
	}
	if queue.Properties.AdvancedSettings.ProvideSpotResourceEnabled == nil || !*queue.Properties.AdvancedSettings.ProvideSpotResourceEnabled || queue.Properties.AdvancedSettings.DequeueStrategy != "BALANCED" || queue.WorkspaceName != "ws-demo" {
		t.Fatalf("unexpected queue: %#v", queue)
	}
}

func TestListSSPQueueWorkloadsBuildsFilter(t *testing.T) {
	var requestPath string
	var requestFilter string
	var orderBy string
	httpClient := &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestPath = r.URL.Path
		requestFilter = r.URL.Query().Get("filter")
		orderBy = r.URL.Query().Get("order_by")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "workloads":[{
    "name":"job-demo","uid":"job-uid","type":"trainingJob","state":"Running","priority":"NORMAL",
    "workspace":{"name":"ws-demo"},
    "tasks":[{"name":"worker","replicas":2,"resource":{"cpu":"32.0","memory":"240.0GiB","accelerate_device_count":2}}],
    "ownership":{"creator_name":"tester"},"create_time":"2026-08-19T06:32:02Z"
  }],
  "total_size":1
}`)),
			Request: r,
		}, nil
	})}
	client := &VirtualClusterClient{
		profiles:   map[string]clientProfile{"d": {Name: "d", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://management.d.example.com"}},
		httpClient: httpClient,
	}
	items, err := client.ListSSPQueueWorkloads(context.Background(), "d", "sub-1", "default", "cn-pj-01", "cluster-a3", "queue-demo", SSPQueueWorkloadQuery{
		Type: "trainingJob", State: "Running", Priority: "NORMAL",
	})
	if err != nil {
		t.Fatalf("ListSSPQueueWorkloads() error = %v", err)
	}
	if requestPath != "/compute/ssp/v1/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a3/queues/queue-demo/workloads" {
		t.Fatalf("request path = %q", requestPath)
	}
	if requestFilter != `type="trainingJob" AND state="Running" AND priority="NORMAL"` || orderBy != "create_time desc" {
		t.Fatalf("filter=%q order_by=%q", requestFilter, orderBy)
	}
	if len(items) != 1 || items[0].Name != "job-demo" || len(items[0].Tasks) != 1 {
		t.Fatalf("unexpected workloads: %#v", items)
	}
}

func TestGetSSPQueueResourceReadsClusterRelationAndNodes(t *testing.T) {
	httpClient := &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "resources": [{
    "id": "cluster-uid",
    "name": "cluster-a3",
    "type": "compute.ssp.v1.cluster",
    "properties": "{\"source\":{\"name\":\"vc-a3-demo\"}}",
    "related_resources": [{
      "resource": {
        "id": "queue-uid",
        "rid": "/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-03/clusters/cluster-a3/queues/queue-demo",
        "name": "queue-demo",
        "type": "compute.ssp.v1.queue",
        "state": "RUNNING",
        "properties": "{\"type\":\"EXCLUSIVE\",\"nodes\":[{\"name\":\"acn-one\"},{\"name\":\"acn-two\"}]}"
      }
    }]
  }]
}`)),
			Request: r,
		}, nil
	})}
	client := &VirtualClusterClient{
		profiles: map[string]clientProfile{
			"pt": {Name: "pt", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://management.pjlab.org.cn"},
		},
		httpClient: httpClient,
	}
	details, err := client.GetSSPQueueResource(context.Background(), "pt", "queue-demo")
	if err != nil {
		t.Fatalf("GetSSPQueueResource returned error: %v", err)
	}
	if details.UID != "queue-uid" || details.ClusterName != "cluster-a3" || details.VClusterName != "vc-a3-demo" || details.Subscription != "sub-1" || len(details.NodeNames) != 2 {
		t.Fatalf("unexpected queue details: %#v", details)
	}
	queues, err := client.ListSSPQueueResources(context.Background(), "pt", "cn-pj-03")
	if err != nil {
		t.Fatalf("ListSSPQueueResources returned error: %v", err)
	}
	if len(queues) != 1 || queues[0].Name != "queue-demo" || queues[0].VClusterName != "vc-a3-demo" {
		t.Fatalf("unexpected queue resources: %#v", queues)
	}
}

func TestListSSPQueueNodesUsesClusterQueueEndpoint(t *testing.T) {
	var requestPath string
	pageSizes := make([]string, 0, 2)
	skips := make([]string, 0, 2)
	var mu sync.Mutex
	httpClient := &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requestPath = r.URL.Path
		pageSizes = append(pageSizes, r.URL.Query().Get("page_size"))
		skips = append(skips, r.URL.Query().Get("skip"))
		mu.Unlock()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "nodes": [{
    "uid": "acn-uid",
    "name": "acn-name",
    "host_ip": "10.140.1.2",
    "machine_type": "h2ls.ru.k10",
    "state": "RUNNING",
    "summary_data": [{"resource_type":"DEVICE","allocated":"4","total":"16","unallocated":"12","unit":"device"}]
  }],
  "total_size": 1
}`)),
			Request: r,
		}, nil
	})}
	client := &VirtualClusterClient{
		profiles: map[string]clientProfile{
			"d": {Name: "d", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://management.d.pjlab.org.cn", ResourceGroup: "default"},
		},
		httpClient: httpClient,
	}

	nodes, err := client.ListSSPQueueNodes(context.Background(), "d", "sub-1", "default", "cn-pj-01", "cluster-a3", "queue-demo")
	if err != nil {
		t.Fatalf("ListSSPQueueNodes returned error: %v", err)
	}
	wantPath := "/compute/ssp/v1/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a3/queues/queue-demo/nodes"
	if requestPath != wantPath || len(pageSizes) != 2 {
		t.Fatalf("unexpected requests: path=%q page_sizes=%#v skips=%#v", requestPath, pageSizes, skips)
	}
	for _, pageSize := range pageSizes {
		if pageSize != "100" {
			t.Fatalf("unexpected page sizes: %#v", pageSizes)
		}
	}
	sort.Strings(skips)
	if skips[0] != "0" || skips[1] != "100" {
		t.Fatalf("unexpected skips: %#v", skips)
	}
	if len(nodes) != 1 || nodes[0].UID != "acn-uid" || nodes[0].HostIP != "10.140.1.2" || len(nodes[0].SummaryData) != 1 {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
}

func TestListSSPQueueNodesContinuesWhenServerCapsPageSize(t *testing.T) {
	httpClient := &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		nodes := make([]map[string]string, 100)
		for index := range nodes {
			nodes[index] = map[string]string{"uid": fmt.Sprintf("node-%d", index), "host_ip": fmt.Sprintf("10.0.0.%d", index)}
		}
		if r.URL.Query().Get("skip") == "100" {
			nodes = []map[string]string{{"uid": "node-100", "host_ip": "10.0.1.0"}}
		}
		body, _ := json.Marshal(map[string]any{"nodes": nodes, "total_size": 101})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    r,
		}, nil
	})}
	client := &VirtualClusterClient{
		profiles: map[string]clientProfile{
			"d": {Name: "d", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://management.d.pjlab.org.cn"},
		},
		httpClient: httpClient,
	}
	nodes, err := client.ListSSPQueueNodes(context.Background(), "d", "sub-1", "default", "cn-pj-01", "cluster-a3", "queue-demo")
	if err != nil {
		t.Fatalf("ListSSPQueueNodes() error = %v", err)
	}
	if len(nodes) != 101 || nodes[100].UID != "node-100" {
		t.Fatalf("nodes = %#v, want both capped pages", nodes)
	}
}

func TestSSPClusterAPIsUseClusterEndpointsAndNormalizeRelations(t *testing.T) {
	requests := make([]string, 0)
	httpClient := &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.Path)
		body := `{}`
		switch r.URL.Path {
		case "/compute/ssp/v1/subscriptions/-/resourceGroups/-/regions/-/clusters":
			body = `{"clusters":[{"id":"/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a3","uid":"cluster-uid","name":"cluster-a3","state":"RUNNING","properties":{"source":{"name":"vc-a3","uid":"vc-uid"},"queue_status":{"num":2},"node_status":{"total":144}}},{"id":"/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-02/clusters/cluster-other","uid":"other-uid","name":"cluster-other","region":"cn-pj-02"}],"total_size":2}`
		case "/compute/ssp/v1/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a3":
			body = `{"id":"/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a3","uid":"cluster-uid","name":"cluster-a3","state":"RUNNING","properties":{"source":{"name":"vc-a3","uid":"vc-uid"}}}`
		case "/compute/ssp/v1/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a3/summary":
			body = `{"summary_data":[{"resource_type":"DEVICE","allocated":"32","total":"144","unallocated":"112","unit":"device"}]}`
		case "/compute/ssp/v1/subscriptions/sub-1/resourceGroups/default/regions/cn-pj-01/clusters/cluster-a3/queues":
			body = `{"queues":[{"uid":"queue-uid","name":"queue-demo","state":"RUNNING","properties":{"type":"EXCLUSIVE","workspace":{"name":"ws-demo","uid":"ws-uid"},"node_status":{"total":4}}}],"total_size":1}`
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	client := &VirtualClusterClient{
		currentProfile: "d",
		profiles: map[string]clientProfile{
			"d": {Name: "d", AccessKey: "ak", SecretKey: "sk", BaseURL: "https://management.d.pjlab.org.cn", Region: "cn-pj-01"},
		},
		httpClient: httpClient,
	}

	clusters, err := client.ListSSPClusters(context.Background(), "cn-pj-01")
	if err != nil || len(clusters) != 1 {
		t.Fatalf("ListSSPClusters() = %#v, %v", clusters, err)
	}
	cluster := clusters[0]
	if cluster.Subscription != "sub-1" || cluster.ResourceGroup != "default" || cluster.Properties.Source.Name != "vc-a3" || cluster.Properties.NodeStatus.Total != 144 {
		t.Fatalf("unexpected cluster: %#v", cluster)
	}
	detail, err := client.GetSSPCluster(context.Background(), cluster)
	if err != nil || detail.Name != "cluster-a3" {
		t.Fatalf("GetSSPCluster() = %#v, %v", detail, err)
	}
	summary, err := client.GetSSPClusterSummary(context.Background(), cluster)
	if err != nil || len(summary) != 1 || summary[0].Total != "144" {
		t.Fatalf("GetSSPClusterSummary() = %#v, %v", summary, err)
	}
	queues, err := client.ListSSPClusterQueues(context.Background(), cluster)
	if err != nil || len(queues) != 1 || queues[0].WorkspaceName != "ws-demo" || queues[0].Properties.NodeStatus.Total != 4 {
		t.Fatalf("ListSSPClusterQueues() = %#v, %v", queues, err)
	}
	if len(requests) != 4 {
		t.Fatalf("request count = %d, want 4: %#v", len(requests), requests)
	}
}
