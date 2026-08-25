package service

import (
	"testing"

	"rayctl/internal/platform"
)

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
