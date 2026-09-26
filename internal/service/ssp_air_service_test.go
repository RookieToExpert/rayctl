package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
