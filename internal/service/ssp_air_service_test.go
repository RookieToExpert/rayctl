package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rayctl/internal/platform"
)

func TestAIRJobVClusterComesFromQueueResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rmh/v1/resources:page" {
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"resources":[{"id":"cluster-uid","name":"cluster-muxi","type":"compute.ssp.v1.cluster","properties":"{\"source\":{\"name\":\"vc-muxi-c550-ailab\"}}","related_resources":[{"resource":{"id":"queue-uid","name":"queue-d-reserved-muxi-h3c-data-producer","type":"compute.ssp.v1.queue","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/cluster-muxi/queues/queue-d-reserved-muxi-h3c-data-producer"}}]}]}`)
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "platform.json")
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","base_url":%q}}}`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	item := SSPAIRJobItem{Queue: "queue-d-reserved-muxi-h3c-data-producer", Cluster: "cluster-muxi", Profile: "test"}
	vc := NewSSPAIRService(client).resolveAIRJobVCluster(context.Background(), item)
	if vc != "vc-muxi-c550-ailab" || item.Cluster != "cluster-muxi" {
		t.Fatalf("VC = %q, cluster = %q", vc, item.Cluster)
	}
}

func TestAIRJobItemUsesLeaderResourceAndQueueCluster(t *testing.T) {
	var job platform.SSPAIRJob
	job.Name = "infer-demo"
	job.WorkspaceName = "ws-demo"
	job.Spec.Queue.Name = "queue-demo"
	job.Spec.Queue.ID = "/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/cluster-demo/queues/queue-demo"
	job.Spec.LWS.Replicas = 4
	job.Status.ReadyReplicas = 3
	job.Spec.LWS.LeaderWorkerTemplate.VolumeMounts = append(job.Spec.LWS.LeaderWorkerTemplate.VolumeMounts, platform.SSPAIRVolumeMount{
		Type: "PV_AFS", ID: "019fabca-cb5e-7577-8dea-c09da02b0cb1", Name: "afs-muxi-pdf", MountPath: "/data-muxi",
	})
	job.Spec.LWS.LeaderWorkerTemplate.Leader.Containers = append(job.Spec.LWS.LeaderWorkerTemplate.Leader.Containers, platform.SSPAIRContainer{
		Name: "leader", Image: platform.SSPAIRImage{Path: "registry/image:tag"}, Resource: platform.SSPAIRResource{
			MachineTypes: []string{"h2ls.ru.k10"}, CPUCount: 32, MemoryGiB: 240, AccelerateDeviceCount: 2, AccelerateDeviceModel: "910C",
		},
	})
	item := NewSSPAIRService(nil).airJobItem(job)
	if item.Cluster != "cluster-demo" || item.Replicas != 4 || item.ReadyReplicas != 3 || item.Resource.CPU != "32" || item.Resource.Memory != "240GiB" || item.Resource.Accelerator != "2 910C" || item.Resource.Image != "registry/image:tag" {
		t.Fatalf("item = %#v", item)
	}
	if len(item.Volumes) != 1 || item.Volumes[0].Endpoint != "csi://019fabca-cb5e-7577-8dea-c09da02b0cb1" {
		t.Fatalf("volumes = %#v", item.Volumes)
	}
}

func TestAIRJobItemSeparatesGroupAndInstanceCounts(t *testing.T) {
	tests := []struct {
		name          string
		desiredGroups int
		currentGroups int
		readyGroups   int
		groupSize     int
		apiInstances  int
		wantGroups    int
		wantInstances int
	}{
		{name: "one group of eight", desiredGroups: 1, currentGroups: 1, readyGroups: 1, groupSize: 8, wantGroups: 1, wantInstances: 8},
		{name: "desired and current groups differ", desiredGroups: 4, currentGroups: 3, readyGroups: 2, groupSize: 8, apiInstances: 24, wantGroups: 3, wantInstances: 32},
		{name: "instances do not replace groups", desiredGroups: 1, groupSize: 8, apiInstances: 8, wantGroups: 1, wantInstances: 8},
		{name: "unknown group size uses API total", desiredGroups: 1, apiInstances: 8, wantGroups: 1, wantInstances: 8},
		{name: "unknown groups stay unknown", apiInstances: 8, wantInstances: 8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var job platform.SSPAIRJob
			job.Spec.LWS.Replicas = test.desiredGroups
			job.Spec.LWS.LeaderWorkerTemplate.Size = test.groupSize
			job.Status.Replicas = test.currentGroups
			job.Status.ReadyReplicas = test.readyGroups
			job.TotalInstances = test.apiInstances
			item := NewSSPAIRService(nil).airJobItem(job)
			if item.Replicas != test.wantGroups || item.DesiredReplicas != test.desiredGroups || item.ReadyReplicas != test.readyGroups || item.GroupSize != test.groupSize || item.TotalInstances != test.wantInstances {
				t.Fatalf("counts = current %d, desired %d, ready %d, size %d, instances %d; want current %d, desired %d, ready %d, size %d, instances %d",
					item.Replicas, item.DesiredReplicas, item.ReadyReplicas, item.GroupSize, item.TotalInstances,
					test.wantGroups, test.desiredGroups, test.readyGroups, test.groupSize, test.wantInstances)
			}
		})
	}
}

func TestFilterAIRJobsByStateIgnoresCaseAndWhitespace(t *testing.T) {
	items := []SSPAIRJobItem{
		{Name: "running", State: "RUNNING"},
		{Name: "pending", State: "Pending"},
		{Name: "running-two", State: "Running"},
	}
	filtered := filterAIRJobsByState(items, " running ")
	if len(filtered) != 2 || filtered[0].Name != "running" || filtered[1].Name != "running-two" {
		t.Fatalf("filtered AIR jobs = %#v", filtered)
	}
	if all := filterAIRJobsByState(items, ""); len(all) != 3 {
		t.Fatalf("unfiltered AIR jobs = %#v", all)
	}
}

func TestAIRGatewayItemBuildsExternalEndpoint(t *testing.T) {
	var gateway platform.SSPAIRGateway
	gateway.Name = "service-demo"
	gateway.Spec.DNATRules = append(gateway.Spec.DNATRules, platform.SSPAIRDNATRule{ExternalIP: "10.1.2.3", ExternalPort: "8080", InternalPort: 80, Protocol: "TCP"})
	item := airGatewayItem(gateway)
	if len(item.DNATRules) != 1 || item.DNATRules[0].External != "10.1.2.3:8080" || item.DNATRules[0].Internal != "" {
		t.Fatalf("item = %#v", item)
	}
}

func TestAIRLooksLikeUUID(t *testing.T) {
	if !airLooksLikeUUID("5abab46b-513d-4b1e-b4cd-7b462d259a22") || airLooksLikeUUID("infer-demo") {
		t.Fatal("airLooksLikeUUID returned unexpected result")
	}
}

func TestSetAIRNamespacesUsesPodMapping(t *testing.T) {
	pods := []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{
		Namespace: "vcluster-host-ns",
		Annotations: map[string]string{
			"vcluster.loft.sh/object-namespace": "workspace-queue-uid",
		},
	}}}
	var namespace, hostNamespace string
	setAIRNamespaces(&namespace, &hostNamespace, pods)
	if namespace != "workspace-queue-uid" || hostNamespace != "vcluster-host-ns" {
		t.Fatalf("namespaces = %q / %q", namespace, hostNamespace)
	}
}

func TestSetAIRWorkersSeparatesComputeAndAuxiliaryInstances(t *testing.T) {
	item := SSPAIRJobItem{Name: "infer-demo", WorkerTotal: 9}
	setAIRWorkers(&item, testAIRWorkers(), nil)
	if len(item.Workers) != 8 || len(item.AuxiliaryWorkers) != 1 || item.WorkerTotal != 9 {
		t.Fatalf("compute=%d auxiliary=%d total=%d", len(item.Workers), len(item.AuxiliaryWorkers), item.WorkerTotal)
	}
	for index, worker := range item.Workers {
		wantRole := "WORKER"
		if index == 0 {
			wantRole = "LEADER"
		}
		if worker.Role != wantRole || worker.Group != "0" || worker.Node != "node-"+strconv.Itoa(index) || worker.HostIP != "10.0.0."+strconv.Itoa(index+1) || worker.PodIP != "10.1.0."+strconv.Itoa(index+1) || worker.Phase != "RUNNING" {
			t.Fatalf("compute instance %d = %#v", index, worker)
		}
	}
	auxiliary := item.AuxiliaryWorkers[0]
	if auxiliary.Name != "infer-demo-metrics" || auxiliary.Role != "AUXILIARY" || auxiliary.Group != "" || auxiliary.Node != "node-metrics" {
		t.Fatalf("auxiliary instance = %#v", auxiliary)
	}
}

func TestSetAIRWorkersEnrichesMappedPodsAndKeepsPlatformNode(t *testing.T) {
	workers := testAIRWorkers()[:2]
	workers[1].Phase = ""
	workers[1].HostIP = ""
	workers[1].IP = ""
	workers[1].RestartCount = 99
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "host-leader", Annotations: map[string]string{"vcluster.loft.sh/object-name": workers[0].Name}},
			Spec:       corev1.PodSpec{NodeName: "host-node-0"},
			Status:     corev1.PodStatus{Phase: corev1.PodPending, HostIP: "10.2.0.1", PodIP: "10.3.0.1"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "host-worker", Annotations: map[string]string{"vcluster.loft.sh/object-name": workers[1].Name}},
			Spec:       corev1.PodSpec{NodeName: "host-node-1"},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning, HostIP: "10.2.0.2", PodIP: "10.3.0.2",
				InitContainerStatuses: []corev1.ContainerStatus{{Name: "init", RestartCount: 2}},
				ContainerStatuses:     []corev1.ContainerStatus{{Name: "inference", RestartCount: 3}},
			},
		},
	}
	item := SSPAIRJobItem{Name: "infer-demo"}
	setAIRWorkers(&item, workers, pods)
	if len(item.Workers) != 2 {
		t.Fatalf("instances = %#v", item.Workers)
	}
	leader := item.Workers[0]
	if leader.Node != "node-0" || leader.Phase != "RUNNING" || leader.HostIP != "10.0.0.1" || leader.PodIP != "10.1.0.1" {
		t.Fatalf("platform fields changed = %#v", leader)
	}
	worker := item.Workers[1]
	if worker.Node != "node-1" || worker.Phase != "Running" || worker.HostIP != "10.2.0.2" || worker.PodIP != "10.3.0.2" || worker.Restarts != 5 {
		t.Fatalf("mapped Pod did not enrich instance = %#v", worker)
	}
	workers[1].NodeInfo.Name = ""
	item = SSPAIRJobItem{Name: "infer-demo"}
	setAIRWorkers(&item, workers[1:], pods)
	if item.Workers[0].Node != "host-node-1" {
		t.Fatalf("missing platform node fallback = %#v", item.Workers[0])
	}
}

func TestAIRWorkerRoleUsesLabelsAndOlderNumericNames(t *testing.T) {
	tests := []struct {
		name      string
		labels    map[string]string
		wantRole  string
		wantGroup string
	}{
		{name: "infer-demo-0", wantRole: "LEADER", wantGroup: "0"},
		{name: "infer-demo-2-0", wantRole: "LEADER", wantGroup: "2"},
		{name: "infer-demo-2-7", wantRole: "WORKER", wantGroup: "2"},
		{name: "infer-demo-metrics", wantRole: "AUXILIARY"},
		{name: "infer-demo-0-metrics", wantRole: "AUXILIARY"},
		{name: "infer-demo-0-7-extra", wantRole: "AUXILIARY"},
		{name: "infer-demo--1", wantRole: "AUXILIARY"},
		{name: "another-job-0", wantRole: "AUXILIARY"},
		{name: "infer-demo-0-1", labels: map[string]string{
			"leaderworkerset.sigs.k8s.io/name":         "another-job",
			"leaderworkerset.sigs.k8s.io/group-index":  "0",
			"leaderworkerset.sigs.k8s.io/worker-index": "1",
		}, wantRole: "AUXILIARY"},
	}
	for _, test := range tests {
		t.Run(test.name+test.labels["leaderworkerset.sigs.k8s.io/name"], func(t *testing.T) {
			worker := platform.SSPAIRWorker{Name: test.name}
			worker.Metadata.Labels = test.labels
			role, group := airWorkerRole("infer-demo", worker)
			if role != test.wantRole || group != test.wantGroup {
				t.Fatalf("role=%q group=%q, want %q / %q", role, group, test.wantRole, test.wantGroup)
			}
		})
	}
}

func TestAIRGetJobsIncludesInstancesWithoutLongDetails(t *testing.T) {
	item, errs, workerRequests := testAIRGetJobsWithoutLongDetails(t, false)
	if len(errs) != 0 {
		t.Fatalf("GetJobs() errors=%v", errs)
	}
	if workerRequests != 1 || len(item.Workers) != 8 || len(item.AuxiliaryWorkers) != 1 || item.WorkerTotal != 9 {
		t.Fatalf("worker requests=%d compute=%d auxiliary=%d total=%d", workerRequests, len(item.Workers), len(item.AuxiliaryWorkers), item.WorkerTotal)
	}
	if item.Replicas != 1 || item.DesiredReplicas != 1 || item.ReadyReplicas != 1 || item.GroupSize != 8 || item.TotalInstances != 8 {
		t.Fatalf("instance counts = %#v", item)
	}
	if item.PodEvidence != nil {
		t.Fatalf("short query collected long Pod evidence: %#v", item.PodEvidence)
	}
}

func TestAIRGetJobsKeepsSummaryWhenWorkersFail(t *testing.T) {
	item, errs, workerRequests := testAIRGetJobsWithoutLongDetails(t, true)
	if workerRequests < 1 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "workers") {
		t.Fatalf("worker requests=%d errors=%v, want workers query and one error", workerRequests, errs)
	}
	if item.Name != "infer-demo" || item.Replicas != 1 || item.DesiredReplicas != 1 || item.GroupSize != 8 || item.TotalInstances != 8 {
		t.Fatalf("summary lost after workers failure: %#v", item)
	}
	if item.PodEvidence != nil {
		t.Fatalf("short query collected long Pod evidence: %#v", item.PodEvidence)
	}
}

func testAIRGetJobsWithoutLongDetails(t *testing.T, failWorkers bool) (*SSPAIRJobItem, []error, int32) {
	t.Helper()
	const airPath = "/air/data/v1/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/ws-demo/airs"
	var workerRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rmh/v1/resources:page":
			fmt.Fprint(w, `{"resources":[{"id":"workspace-uid","name":"ws-demo","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/ws-demo"}]}`)
		case airPath:
			fmt.Fprint(w, `{"airs":[{"name":"infer-demo","uid":"air-uid"}],"total_size":1}`)
		case airPath + "/infer-demo":
			fmt.Fprint(w, `{"name":"infer-demo","uid":"air-uid","spec":{"lws":{"replicas":1,"leader_worker_template":{"size":8}}},"status":{"state":"RUNNING","replicas":1,"ready_replicas":1},"total_instances":8}`)
		case airPath + "/infer-demo/workers":
			workerRequests.Add(1)
			if failWorkers {
				http.Error(w, "workers unavailable", http.StatusServiceUnavailable)
				return
			}
			if r.URL.Query().Get("page_size") != "20" {
				t.Errorf("worker page size = %q, want 20", r.URL.Query().Get("page_size"))
			}
			if err := json.NewEncoder(w).Encode(struct {
				Workers   []platform.SSPAIRWorker `json:"workers"`
				TotalSize int                     `json:"total_size"`
			}{Workers: testAIRWorkers(), TotalSize: 9}); err != nil {
				t.Errorf("encode workers: %v", err)
			}
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "platform.json")
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","region":"cn-pj-01","resource_group":"default","base_url":%q,"kubernetes_base_url":%q}}}`, server.URL, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	items, errs := NewSSPAIRService(client).GetJobs(context.Background(), []string{"infer-demo"}, "cn-pj-01", "ws-demo", false, 20)
	if len(items) != 1 || items[0] == nil {
		t.Fatalf("items=%#v errors=%v", items, errs)
	}
	return items[0], errs, workerRequests.Load()
}

func testAIRWorkers() []platform.SSPAIRWorker {
	workers := make([]platform.SSPAIRWorker, 0, 9)
	for index := 0; index < 8; index++ {
		name := "infer-demo-0"
		if index > 0 {
			name += "-" + strconv.Itoa(index)
		}
		worker := platform.SSPAIRWorker{
			Name: name, Phase: "RUNNING", HostIP: "10.0.0." + strconv.Itoa(index+1), IP: "10.1.0." + strconv.Itoa(index+1),
		}
		worker.Metadata.Labels = map[string]string{
			"leaderworkerset.sigs.k8s.io/name":         "infer-demo",
			"leaderworkerset.sigs.k8s.io/group-index":  "0",
			"leaderworkerset.sigs.k8s.io/worker-index": strconv.Itoa(index),
		}
		worker.NodeInfo.Name = "node-" + strconv.Itoa(index)
		workers = append(workers, worker)
	}
	metrics := platform.SSPAIRWorker{Name: "infer-demo-metrics", Phase: "RUNNING"}
	metrics.NodeInfo.Name = "node-metrics"
	workers = append(workers, metrics)
	return workers
}
