package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"rayctl/internal/platform"
)

func TestNodeWorkloadsDescribeAllPodsAndIP(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker"}, Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.1.2.3"}}}}
	pod := locateTestPod("user", "aid-pod")
	pod.Spec.NodeName = "worker"
	pod.Spec.Containers = []corev1.Container{{Name: "main", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")}}}}
	pod.Status.StartTime = &metav1.Time{Time: time.Now().Add(-time.Minute)}
	system := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "system", Namespace: "kube-system"}, Spec: corev1.PodSpec{NodeName: "worker"}}
	other := locateTestPod("user", "other-node")
	other.Spec.NodeName = "other"
	client := fake.NewSimpleClientset(node, pod, system, other)
	r, err := NewNodeService(client).WithWorkloads().Describe(context.Background(), "10.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Workloads) != 2 || r.Workloads[0].Type != "OTHER" || r.Workloads[1].Type != "AID" {
		t.Fatalf("%+v", r.Workloads)
	}
	row := r.Workloads[1]
	if row.AcceleratorRequests["nvidia.com/gpu"] != "2" || row.RuntimeSeconds < 60 || row.Source != "k8s" {
		t.Fatalf("%+v", row)
	}
	if len(client.Actions()) != 2 {
		t.Fatalf("expected one node list and one pod list: %+v", client.Actions())
	}
	if _, err := NewNodeService(client).WithWorkloads().Describe(context.Background(), "missing"); err == nil {
		t.Fatal("missing node succeeded")
	}
}

type workloadPlatformFake struct {
	queueCalls atomic.Int32
	fail       bool
}

func (*workloadPlatformFake) CurrentProfileName() string { return "test" }
func (f *workloadPlatformFake) GetSSPQueueResource(ctx context.Context, _, name string) (*platform.SSPQueueResourceDetails, error) {
	f.queueCalls.Add(1)
	if f.fail {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &platform.SSPQueueResourceDetails{Name: name, WorkspaceName: "ws-real", VClusterName: "vc-real"}, nil
}
func (*workloadPlatformFake) ResolveUsernames(context.Context, []string) (map[string]string, error) {
	return map[string]string{"019bdf2f-a0d9-7122-8452-b3889bc1372a": "alice"}, nil
}
func (*workloadPlatformFake) ResolveDisplayNamesWithProfiles(context.Context, []string) (map[string]string, map[string]string, error) {
	return nil, nil, errors.New("VC unavailable")
}

func TestNodeWorkloadBatchEnrichment(t *testing.T) {
	client := &workloadPlatformFake{}
	results := []NodeDescribeQueryResult{{Details: &NodeDescribe{Workloads: []NodeWorkload{{Pod: "one", Queue: "q", Creator: "019bdf2f-a0d9-7122-8452-b3889bc1372a", Source: "k8s"}, {Pod: "two", Queue: "q", Source: "k8s"}}}}}
	EnrichNodeWorkloads(context.Background(), results, client)
	if client.queueCalls.Load() != 1 {
		t.Fatal("queue lookup not deduplicated")
	}
	for _, row := range results[0].Details.Workloads {
		if row.Workspace != "ws-real" || row.VC != "vc-real" || row.Source != "platform" {
			t.Fatalf("%+v", row)
		}
	}
	if results[0].Details.Workloads[0].Creator != "alice" {
		t.Fatal("creator not resolved")
	}
}

func TestNodeWorkloadSharedTimeout(t *testing.T) {
	client := &workloadPlatformFake{fail: true}
	results := []NodeDescribeQueryResult{{Details: &NodeDescribe{Workloads: []NodeWorkload{{Pod: "one", Queue: "q1", Source: "k8s"}, {Pod: "two", Queue: "q2", Source: "k8s"}}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	EnrichNodeWorkloads(ctx, results, client)
	if time.Since(start) > time.Second {
		t.Fatal("deadline not shared")
	}
	if len(results[0].Details.Warnings) == 0 || len(results[0].Details.Workloads) != 2 || results[0].Details.Workloads[0].Source != "k8s" {
		t.Fatalf("%+v", results)
	}
}

func TestNodeWorkloadOwnerAndTypes(t *testing.T) {
	controller := true
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent", Controller: &controller}}}}
	rows := nodeWorkloadsFromPods([]corev1.Pod{pod})
	if rows[0].Type != "OTHER" || rows[0].Workload != "agent" {
		t.Fatalf("%+v", rows)
	}
	for _, kind := range []string{"AIT", "ATE", "AID", "AIR", "ECS"} {
		pod.Labels = map[string]string{sspWorkloadTypeLabel: kind, sspWorkloadNameLabel: "job"}
		if row := nodeWorkloadsFromPods([]corev1.Pod{pod})[0]; row.Type != kind || row.Workload != "job" {
			t.Fatalf("%+v", row)
		}
	}
}

func TestNodeWorkloadECSBusinessIdentity(t *testing.T) {
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "virt-launcher-ecs-b397575f3132f5a8-z9cvj", Namespace: "ns-tenant",
		Annotations: map[string]string{
			"resource.compute.sensecore.cn/name": "ecs-yunwei",
			"resource.compute.sensecore.cn/uid":  "019fc69a-98d5-70b4-9745-957244ee092f",
		},
		OwnerReferences: []metav1.OwnerReference{{Kind: "VirtualMachineInstance", Name: "ecs-b397575f3132f5a8", UID: "vmi-uid"}},
	}}
	row := nodeWorkloadsFromPods([]corev1.Pod{pod})[0]
	if row.Type != "ECS" || row.Workload != "ecs-yunwei" || row.WorkloadUID != "019fc69a-98d5-70b4-9745-957244ee092f" || row.Pod != pod.Name || row.Namespace != pod.Namespace {
		t.Fatalf("ECS workload = %+v", row)
	}
}

func TestNodeWorkloadStatusShowsContainerReason(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "metax-data-exporter-97jwn", Namespace: "kube-system"},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "exporter",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}},
			}},
		},
	}
	row := nodeWorkloadsFromPods([]corev1.Pod{pod})[0]
	if row.Phase != "Pending" || row.Status != "ContainerCreating" {
		t.Fatalf("phase=%q status=%q", row.Phase, row.Status)
	}
	pod.Status.ContainerStatuses[0].State.Waiting.Reason = "CrashLoopBackOff"
	if got := nodeWorkloadsFromPods([]corev1.Pod{pod})[0].Status; got != "CrashLoopBackOff" {
		t.Fatalf("status=%q", got)
	}
	now := metav1.Now()
	pod.DeletionTimestamp = &now
	if got := nodeWorkloadsFromPods([]corev1.Pod{pod})[0].Status; got != "Terminating" {
		t.Fatalf("status=%q", got)
	}
}

func TestNodeWorkloadStatusAllowsStartedInitSidecar(t *testing.T) {
	policy := corev1.ContainerRestartPolicyAlways
	started := true
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers:     []corev1.Container{{Name: "app"}},
			InitContainers: []corev1.Container{{Name: "sidecar", RestartPolicy: &policy}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: "sidecar", Started: &started,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}},
		},
	}
	if got := nodePodStatus(pod); got != "Running" {
		t.Fatalf("running init sidecar reported as %q", got)
	}
	started = false
	pod.Status.InitContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
	if got := nodePodStatus(pod); got != "Init:CrashLoopBackOff" {
		t.Fatalf("failed init sidecar reported as %q", got)
	}
}
