package service

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"rayctl/internal/platform"
)

func TestMatchSSPWorkspaceByNameAndUID(t *testing.T) {
	items := []platform.SSPWorkspace{{Name: "ws-demo", UID: "workspace-uid"}}
	for _, identifier := range []string{"ws-demo", "workspace-uid", "demo"} {
		item, err := matchSSPWorkspace(identifier, items)
		if err != nil || item.Name != "ws-demo" {
			t.Fatalf("matchSSPWorkspace(%q) = %#v, %v", identifier, item, err)
		}
	}
}

func TestWorkspaceItemUsesWorkspaceQueueUID(t *testing.T) {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "runtime-namespace",
		Labels: map[string]string{
			sspWorkspaceNameLabel:     "ws-demo",
			sspWorkspaceQueueUIDLabel: "workspace-queue-uid",
		},
	}}
	resourceService := &SSPResourceService{
		clientset: fake.NewSimpleClientset(namespace),
		sspBase:   NewSSPJobService(fake.NewSimpleClientset(namespace), nil),
	}

	item := resourceService.workspaceItem(context.Background(), platform.SSPWorkspace{
		Name:        "ws-demo",
		UID:         "platform-workspace-uid",
		ClusterName: "vc-demo",
	})

	if item.UID != "workspace-queue-uid" {
		t.Fatalf("workspace UID = %q, want workspace queue UID", item.UID)
	}
}

func TestMatchSSPClusterByNameUIDAndFragment(t *testing.T) {
	items := []platform.SSPCluster{{Name: "cluster-a3", UID: "cluster-uid"}}
	for _, identifier := range []string{"cluster-a3", "cluster-uid", "a3"} {
		item, err := matchSSPCluster(identifier, items)
		if err != nil || item.Name != "cluster-a3" {
			t.Fatalf("matchSSPCluster(%q) = %#v, %v", identifier, item, err)
		}
	}
}

func TestMatchSSPQueueAcceptsVolcanoQueueName(t *testing.T) {
	items := []SSPQueueItem{{Name: "queue-demo", UID: "queue-uid", Workspace: "ws-demo"}}
	item, err := matchSSPQueue("ssp-queue-uid", items)
	if err != nil || item.Name != "queue-demo" {
		t.Fatalf("matchSSPQueue() = %#v, %v", item, err)
	}
}

func TestQueueNodeListItemUsesNodeLabels(t *testing.T) {
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "host-10-140-1-2",
			Labels: map[string]string{
				"resource.compute.sensecore.cn/acn-uid": "acn-uid",
				sspMachineTypeLabel:                     "h2ls.ru.k10",
				"node.kubernetes.io/npu.chip.name":      "Ascend910C",
			},
		},
		Status: corev1.NodeStatus{
			Addresses:  []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.140.1.2"}},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}
	item := queueNodeListItem(node)
	if item.HostIP != "10.140.1.2" || item.State != "Ready" || item.MachineType != "h2ls.ru.k10" || item.Model != "Ascend910C" || item.UID != "acn-uid" {
		t.Fatalf("unexpected node item: %#v", item)
	}
}

func TestQueueNodeListItemsEnrichesHostnameAndModel(t *testing.T) {
	kubeNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "host-10-140-1-2",
			Labels: map[string]string{
				"resource.compute.sensecore.cn/acn-uid": "acn-uid",
				sspMachineTypeLabel:                     "h2ls.ru.k10",
				"accelerator-type":                      "module-910c-8",
			},
		},
		Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.140.1.2"}}},
	}
	service := &SSPResourceService{clientset: fake.NewSimpleClientset(kubeNode)}
	nodes := []platform.SSPQueueNode{{UID: "acn-uid", Name: "acn-name", HostIP: "10.140.1.2", MachineType: "h2ls.ru.k10", State: "RUNNING"}}

	items := service.queueNodeListItems(context.Background(), nodes, true)

	if len(items) != 1 || items[0].HostName != "host-10-140-1-2" || items[0].Model != "module-910c-8" || items[0].Name != "acn-name" {
		t.Fatalf("unexpected node items: %#v", items)
	}
}

func TestQueueNodeListItemsFromKubeUsesQueueUIDLabel(t *testing.T) {
	matching := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "host-10-140-1-2",
		Labels: map[string]string{
			sspQueueUIDLabel:   "queue-uid",
			"accelerator-type": "module-910c-8",
		},
	}}
	other := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "host-10-140-1-3",
		Labels: map[string]string{sspQueueUIDLabel: "other-queue"},
	}}
	service := &SSPResourceService{}
	service.SetQueueNodeClientResolver(func(SSPQueueItem) (kubernetes.Interface, error) {
		return fake.NewSimpleClientset(matching, other), nil
	})

	items, ok := service.queueNodeListItemsFromKube(context.Background(), SSPQueueItem{UID: "queue-uid"})
	if !ok || len(items) != 1 || items[0].HostName != matching.Name || items[0].HostIP != "10.140.1.2" || items[0].Model != "module-910c-8" {
		t.Fatalf("queueNodeListItemsFromKube() = %#v, %v", items, ok)
	}
}

func TestHostIPFromNodeName(t *testing.T) {
	if got := hostIPFromNodeName("host-10-140-216-1"); got != "10.140.216.1" {
		t.Fatalf("hostIPFromNodeName() = %q", got)
	}
	if got := hostIPFromNodeName("worker-0"); got != "" {
		t.Fatalf("hostIPFromNodeName(non-host) = %q", got)
	}
}

func TestQueueSchedulingLabels(t *testing.T) {
	enabled := true
	if got := formatSSPSpotLending(&enabled); got != "开启" {
		t.Fatalf("formatSSPSpotLending() = %q", got)
	}
	for value, want := range map[string]string{
		"": "高利用率（默认）", "HIGH_UTILIZATION": "高利用率", "STRONG_PRIORITY": "强优先级", "BALANCED": "均衡",
	} {
		if got := formatSSPDequeuePolicy(value); got != want {
			t.Fatalf("formatSSPDequeuePolicy(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestFormatSSPWorkloadResources(t *testing.T) {
	var workload platform.SSPQueueWorkload
	workload.Tasks = append(workload.Tasks, struct {
		Name     string `json:"name"`
		Replicas int    `json:"replicas"`
		Resource struct {
			MachineTypes          []string `json:"machine_types"`
			CPU                   any      `json:"cpu"`
			Memory                any      `json:"memory"`
			AccelerateDeviceCount any      `json:"accelerate_device_count"`
		} `json:"resource"`
	}{Name: "worker", Replicas: 2})
	workload.Tasks[0].Resource.CPU = "32.0"
	workload.Tasks[0].Resource.Memory = "240.0GiB"
	workload.Tasks[0].Resource.AccelerateDeviceCount = 2
	workload.Tasks[0].Resource.MachineTypes = []string{"h2ls.ru.k10"}
	if got := formatSSPWorkloadResources(workload); got != "worker×2 32C/240GiB/2ACC/h2ls.ru.k10" {
		t.Fatalf("formatSSPWorkloadResources() = %q", got)
	}
}

func TestSSPQueueNodeResource(t *testing.T) {
	node := platform.SSPQueueNode{}
	node.SummaryData = append(node.SummaryData, struct {
		ResourceType string `json:"resource_type"`
		Allocated    string `json:"allocated"`
		Total        string `json:"total"`
		Unallocated  string `json:"unallocated"`
		Unit         string `json:"unit"`
	}{ResourceType: "DEVICE", Allocated: "4", Total: "16", Unallocated: "12", Unit: "device"})

	allocated, total, unallocated := sspQueueNodeResource(node, "device")
	if allocated != "4" || total != "16" || unallocated != "12" {
		t.Fatalf("resource = %q/%q/%q", allocated, total, unallocated)
	}
}

func TestQueueItemKeepsPlatformNodeCount(t *testing.T) {
	queue := platform.SSPQueue{Name: "queue-demo"}
	queue.Properties.NodeStatus.Total = 144
	item := (&SSPResourceService{}).queueItem(context.Background(), platform.SSPWorkspace{Name: "ws-demo"}, queue, "vc-demo", false)
	if item.NodeCount != 144 {
		t.Fatalf("NodeCount = %d, want 144", item.NodeCount)
	}
}

func TestLikelyQueueWorkspaces(t *testing.T) {
	workspaces := []platform.SSPWorkspace{
		{Name: "ws-d-a3-ai4s"},
		{Name: "ws-d-muxi-830pdf"},
		{Name: "ws-t-wamcritic"},
		{Name: "ws-other"},
	}
	for queueName, want := range map[string]string{
		"queue-d-reserved-a3-ai4s":    "ws-d-a3-ai4s",
		"queue-d-elastic-muxi-830pdf": "ws-d-muxi-830pdf",
		"queue-t-reserved-wamcritic":  "ws-t-wamcritic",
	} {
		got := likelyQueueWorkspaces(queueName, workspaces)
		if len(got) != 1 || got[0].Name != want {
			t.Fatalf("likelyQueueWorkspaces(%q) = %#v, want %q", queueName, got, want)
		}
	}
}

func TestInferWorkspaceNameFromQueue(t *testing.T) {
	for queue, want := range map[string]string{
		"queue-d-reserved-a3-llm-share":   "ws-d-a3-llm-share",
		"queue-d-elastic-muxi-830pdf":     "ws-d-muxi-830pdf",
		"queue-t-exclusive-wamcritic":     "ws-t-wamcritic",
		"queue-without-known-type-marker": "-",
	} {
		if got := inferWorkspaceNameFromQueue(queue); got != want {
			t.Fatalf("inferWorkspaceNameFromQueue(%q) = %q, want %q", queue, got, want)
		}
	}
}
