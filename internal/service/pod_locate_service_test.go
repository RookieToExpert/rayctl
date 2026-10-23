package service

import (
	"context"
	"errors"
	"rayctl/internal/platform"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func locateTestPod(ns, name string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{
		sspWorkloadTypeLabel: "aid", sspWorkloadNameLabel: "real-workload", sspWorkloadUIDLabel: "uid",
	}}, Status: corev1.PodStatus{PodIP: "10.0.0.1", Phase: corev1.PodRunning}}
}

func TestPodLocateNameAndIP(t *testing.T) {
	for _, tc := range []struct{ id, ns, verb, selector string }{
		{"pod", "", "list", "metadata.name=pod"},
		{"host/pod", "", "get", ""},
		{"pod", "host", "get", ""},
		{"10.0.0.1", "", "list", "status.podIP=10.0.0.1"},
		{"10.0.0.1", "host", "list", "status.podIP=10.0.0.1"},
	} {
		t.Run(tc.id+tc.ns, func(t *testing.T) {
			client := fake.NewSimpleClientset(locateTestPod("host", "pod"))
			r, err := NewPodLocateService(client, nil).Get(context.Background(), tc.id, tc.ns, false)
			if err != nil {
				t.Fatal(err)
			}
			if r.WorkloadType != "AID" || r.WorkloadName != "real-workload" || r.HostPod != "pod" || r.Status != "Running" || r.Phase != "Running" {
				t.Fatalf("result: %+v", r)
			}
			actions := client.Actions()
			if len(actions) != 1 || actions[0].GetVerb() != tc.verb {
				t.Fatalf("actions: %+v", actions)
			}
			if tc.selector != "" && actions[0].(ktesting.ListAction).GetListRestrictions().Fields.String() != tc.selector {
				t.Fatal("missing exact field selector")
			}
		})
	}
}

func TestPodLocateStatus(t *testing.T) {
	deleting := metav1.NewTime(time.Now().Add(-30 * time.Minute))
	for _, tc := range []struct {
		name       string
		phase      corev1.PodPhase
		reason     string
		state      corev1.ContainerState
		deleting   bool
		wantStatus string
	}{
		{name: "running", phase: corev1.PodRunning, wantStatus: "Running"},
		{name: "deleting while phase still running", phase: corev1.PodRunning, deleting: true, wantStatus: "Terminating"},
		{name: "deleting overrides container reason", phase: corev1.PodRunning, state: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}, deleting: true, wantStatus: "Terminating"},
		{name: "creating", phase: corev1.PodPending, state: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}, wantStatus: "ContainerCreating"},
		{name: "crash loop", phase: corev1.PodRunning, state: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}, wantStatus: "CrashLoopBackOff"},
		{name: "completed", phase: corev1.PodSucceeded, state: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed"}}, wantStatus: "Completed"},
		{name: "failed", phase: corev1.PodFailed, reason: "Evicted", wantStatus: "Evicted"},
		{name: "deleting on lost node", phase: corev1.PodRunning, reason: "NodeLost", deleting: true, wantStatus: "Unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, identifier := range []string{"pod", "host/pod"} {
				pod := locateTestPod("host", "pod")
				pod.Status.Phase, pod.Status.Reason = tc.phase, tc.reason
				pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{State: tc.state}}
				if tc.deleting {
					pod.DeletionTimestamp = &deleting
				}
				result, err := NewPodLocateService(fake.NewSimpleClientset(pod), nil).Get(context.Background(), identifier, "", false)
				if err != nil {
					t.Fatal(err)
				}
				if result.Status != tc.wantStatus || result.Phase != string(tc.phase) || !result.Ready {
					t.Fatalf("%s: status=%q phase=%q ready=%v; want status=%q phase=%q ready=true", identifier, result.Status, result.Phase, result.Ready, tc.wantStatus, tc.phase)
				}
				if (result.DeletingFor != "") != tc.deleting {
					t.Fatalf("%s: deleting for=%q; deleting=%v", identifier, result.DeletingFor, tc.deleting)
				}
			}
		})
	}
}

func TestPodLocateErrors(t *testing.T) {
	client := fake.NewSimpleClientset(locateTestPod("a", "pod"), locateTestPod("b", "pod"))
	s := NewPodLocateService(client, nil)
	for _, id := range []string{"pod", "10.0.0.1"} {
		_, err := s.Get(context.Background(), id, "", false)
		if err == nil || !strings.Contains(err.Error(), "a/pod, b/pod") {
			t.Fatalf("ambiguity: %v", err)
		}
	}
	for _, tc := range []struct{ id, ns string }{{"missing", ""}, {"a/pod", "b"}, {"a/b/c", ""}, {"", ""}} {
		if _, err := s.Get(context.Background(), tc.id, tc.ns, false); err == nil {
			t.Fatalf("expected error for %+v", tc)
		}
	}
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("denied"))
	})
	_, err := s.Get(context.Background(), "pod", "", false)
	if !apierrors.IsForbidden(err) {
		t.Fatalf("403 lost: %v", err)
	}
}

type failingPodResolver struct{}

func (failingPodResolver) ResolveDisplayNamesWithProfiles(context.Context, []string) (map[string]string, map[string]string, error) {
	return nil, nil, errors.New("platform offline")
}

func TestPodLocatePlatformDegrades(t *testing.T) {
	pod := locateTestPod("host", "pod")
	pod.Annotations = map[string]string{sspVClusterNameAnno: "vc-019bdf2f-a0d9-7122-8452-b3889bc1372a"}
	s := NewPodLocateService(fake.NewSimpleClientset(pod), nil)
	s.resolver = failingPodResolver{}
	r, err := s.Get(context.Background(), "host/pod", "", false)
	if err != nil || r.HostPod != "pod" || len(r.Warnings) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestPodLocatePVCNamespaceAndCancellation(t *testing.T) {
	pod := locateTestPod("host", "pod")
	pod.Spec.Volumes = []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}
	client := fake.NewSimpleClientset(pod)
	r, err := NewPodLocateService(client, nil).Get(context.Background(), "host/pod", "", true)
	if err != nil || len(r.PVCs) != 1 || r.PVCs[0].Name != "data" || r.PVCs[0].Warning == "" {
		t.Fatalf("%+v %v", r, err)
	}
	if a := client.Actions()[1]; a.GetNamespace() != "host" || a.GetVerb() != "get" {
		t.Fatalf("not namespaced: %+v", a)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewPodLocateService(client, nil).Get(ctx, "host/pod", "", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestPodLocateMetadata(t *testing.T) {
	for _, tc := range []struct{ label, want string }{{"training-job", "AIT"}, {"aid", "AID"}, {"inference-service", "AIR"}, {"ate", "ATE"}, {"ecs", "ECS"}, {"unrecognized", "UNKNOWN"}} {
		pod := locateTestPod("host", "pod")
		pod.Labels[sspWorkloadTypeLabel] = tc.label
		pod.Annotations = map[string]string{"vcluster.loft.sh/object-name": "vc-pod", "vcluster.loft.sh/object-namespace": "vc-ns", "identity.compute.sensecore.cn/user-name": "alice"}
		pod.DeletionTimestamp = &metav1.Time{Time: time.Now().Add(-time.Minute)}
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{RestartCount: 3}}
		r := podLocateResult(*pod)
		if r.WorkloadType != tc.want || r.VCPod != "vc-pod" || r.Creator != "alice" || !r.Ready || r.Restarts != 3 || r.DeletingFor == "" {
			t.Fatalf("%+v", r)
		}
	}
}

func TestPodLocateQueueEnrichment(t *testing.T) {
	pod := locateTestPod("host", "pod")
	pod.Labels[nodeQueueNameLabelKey] = "queue-a"
	s := NewPodLocateService(fake.NewSimpleClientset(pod), nil)
	s.queueLookup = func(ctx context.Context, name string) (*platform.SSPQueueResourceDetails, error) {
		if name != "queue-a" {
			t.Fatalf("queue: %s", name)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing enrichment deadline")
		}
		return &platform.SSPQueueResourceDetails{Name: name, VClusterName: "vc-a", WorkspaceName: "ws-a", ClusterName: "cluster-a"}, nil
	}
	r, err := s.Get(context.Background(), "host/pod", "", false)
	if err != nil || r.VC != "vc-a" || r.Workspace != "ws-a" || r.Cluster != "cluster-a" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestPodLocatePaginationCycle(t *testing.T) {
	client := fake.NewSimpleClientset()
	calls := 0
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		tokens := []string{"A", "B", "A"}
		if calls >= len(tokens) {
			t.Fatal("pagination did not stop")
		}
		token := tokens[calls]
		calls++
		return true, &corev1.PodList{ListMeta: metav1.ListMeta{Continue: token}}, nil
	})
	_, err := NewPodLocateService(client, nil).Get(context.Background(), "pod", "", false)
	if err == nil || !strings.Contains(err.Error(), "token repeated") {
		t.Fatalf("%v", err)
	}
}

func TestPodLocateECSBusinessIdentityWithoutDomain(t *testing.T) {
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace: "ns-tenant", Name: "virt-launcher-ecs-b397575f3132f5a8-z9cvj",
		Annotations: map[string]string{
			"resource.compute.sensecore.cn/name": "ecs-yunwei",
			"resource.compute.sensecore.cn/uid":  "019fc69a-98d5-70b4-9745-957244ee092f",
		},
		Labels:          map[string]string{"resourcemanager_id": "019fc69a-98d5-70b4-9745-957244ee092f"},
		OwnerReferences: []metav1.OwnerReference{{Kind: "VirtualMachineInstance", Name: "ecs-b397575f3132f5a8", UID: "kubernetes-vmi-uid"}},
	}}
	r := podLocateResult(pod)
	if r.WorkloadType != "ECS" || r.WorkloadName != "ecs-yunwei" || r.WorkloadUID != "019fc69a-98d5-70b4-9745-957244ee092f" {
		t.Fatalf("ECS identity = %+v", r)
	}
	if len(r.Suggestions) != 1 || r.Suggestions[0] != "rayctl ecs get ecs-yunwei" {
		t.Fatalf("ECS suggestions = %v", r.Suggestions)
	}
}

func TestPodLocateECSMetadataPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name              string
		annotations       map[string]string
		labels            map[string]string
		wantName, wantUID string
	}{
		{
			name:        "resource annotations",
			annotations: map[string]string{"resource.compute.sensecore.cn/name": "business", "resource.compute.sensecore.cn/uid": "resource-uid"},
			labels:      map[string]string{sspWorkloadNameLabel: "workload", sspWorkloadUIDLabel: "workload-uid", "vm.kubevirt.io/name": "vm-business", "kubevirt.io/domain": "domain", "resourcemanager_id": "manager-uid"},
			wantName:    "business", wantUID: "resource-uid",
		},
		{
			name:     "explicit workload",
			labels:   map[string]string{sspWorkloadNameLabel: "workload", sspWorkloadUIDLabel: "workload-uid", "vm.kubevirt.io/name": "vm-business", "kubevirt.io/domain": "domain", "resourcemanager_id": "manager-uid"},
			wantName: "workload", wantUID: "manager-uid",
		},
		{
			name:     "business VM label",
			labels:   map[string]string{"vm.kubevirt.io/name": "vm-business", "kubevirt.io/domain": "domain", sspWorkloadUIDLabel: "workload-uid"},
			wantName: "vm-business", wantUID: "workload-uid",
		},
		{
			name:        "domain",
			annotations: map[string]string{"kubevirt.io/domain": "domain"},
			wantName:    "domain", wantUID: "owner-uid",
		},
		{
			name:        "VMI owner before legacy job inference",
			annotations: map[string]string{"volcano.sh/job-name": "unrelated-job"},
			wantName:    "vm-technical", wantUID: "owner-uid",
		},
		{name: "VMI owner", wantName: "vm-technical", wantUID: "owner-uid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name: "launcher", Annotations: tc.annotations, Labels: tc.labels,
				OwnerReferences: []metav1.OwnerReference{{Kind: "VirtualMachineInstance", Name: "vm-technical", UID: "owner-uid"}},
			}}
			r := podLocateResult(pod)
			if r.WorkloadType != "ECS" || r.WorkloadName != tc.wantName || r.WorkloadUID != tc.wantUID {
				t.Fatalf("identity = %+v, want ECS %q %q", r, tc.wantName, tc.wantUID)
			}
		})
	}
}

func TestPodLocateECSRequiresMetadataEvidence(t *testing.T) {
	for _, tc := range []struct {
		name               string
		labels             map[string]string
		annotations        map[string]string
		wantType, wantName string
	}{
		{name: "domain identifies VM", annotations: map[string]string{"kubevirt.io/domain": "vm-domain"}, wantType: "ECS", wantName: "vm-domain"},
		{name: "launcher label identifies VM", labels: map[string]string{"kubevirt.io": "virt-launcher", "vm.kubevirt.io/name": "ecs-business"}, wantType: "ECS", wantName: "ecs-business"},
		{name: "name prefix alone", wantType: "UNKNOWN"},
		{name: "resource annotation alone", annotations: map[string]string{"resource.compute.sensecore.cn/name": "ecs-business"}, wantType: "UNKNOWN"},
		{name: "business label alone", labels: map[string]string{"vm.kubevirt.io/name": "ecs-business"}, wantType: "UNKNOWN"},
		{name: "explicit non-VM workload", labels: map[string]string{sspWorkloadTypeLabel: "aid", sspWorkloadNameLabel: "aid-business", "kubevirt.io": "virt-launcher"}, annotations: map[string]string{"resource.compute.sensecore.cn/name": "ecs-business"}, wantType: "AID", wantName: "aid-business"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "virt-launcher-ecs-name", Labels: tc.labels, Annotations: tc.annotations}}
			r := podLocateResult(pod)
			if r.WorkloadType != tc.wantType || r.WorkloadName != tc.wantName {
				t.Fatalf("identity = %+v, want %s %q", r, tc.wantType, tc.wantName)
			}
		})
	}
}
