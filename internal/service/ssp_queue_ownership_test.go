package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"rayctl/internal/platform"
)

func ownershipUsage(name string) SSPQueueNodeUsageItem {
	return SSPQueueNodeUsageItem{HostName: name, acnUID: "acn-" + name, CPUAllocated: "0", CPUTotal: "16", MemoryAllocated: "0GiB", MemoryTotal: "32GiB", AcceleratorAllocated: "0", AcceleratorTotalText: "8", AcceleratorTotal: 8, AcceleratorFree: 8}
}

func ownershipNode(name string) corev1.Node {
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{queueOwnershipACNLabel: "acn-" + name, queueOwnershipVCLabel: "vc-id"}}}
}

func ownershipPod(node, uid string) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-" + node, Namespace: "apps", Labels: map[string]string{nodeQueueUIDLabelKey: uid}}, Spec: corev1.PodSpec{NodeName: node}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
}

func ownershipResult(items ...SSPQueueNodeUsageItem) *SSPQueueNodeUsageResult {
	return &SSPQueueNodeUsageResult{Queue: SSPQueueItem{Name: "queue-self", UID: "self-uid", Profile: "test", VCluster: "vc-test"}, SharedVCPool: true, NodeCount: len(items), vclusterUID: "vc-id", Items: items,
		Quota: &platform.SSPQueueQuota{Resources: []platform.SSPResourceSummary{{ResourceType: "CPU", Total: "128", Allocated: "128", Unallocated: "0"}}}}
}

func TestQueueUsageDefaultShowsOwnedIncludingFullNodes(t *testing.T) {
	for _, freeOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(freeOnly), func(t *testing.T) {
			items := []SSPQueueNodeUsageItem{ownershipUsage("own-full"), ownershipUsage("own-free"), ownershipUsage("public"), ownershipUsage("other")}
			items[0].CPUAllocated, items[0].AcceleratorFree = "16", 0
			items[0].AcceleratorAllocated = "8"
			items[1].CPUAllocated, items[1].AcceleratorAllocated, items[1].AcceleratorFree = "8", "4", 4
			nodes := []corev1.Node{ownershipNode("own-full"), ownershipNode("own-free"), ownershipNode("public"), ownershipNode("other")}
			pods := []corev1.Pod{ownershipPod("own-full", "self-uid"), ownershipPod("own-free", "self-uid"), ownershipPod("other", "other-uid")}
			client := fake.NewClientset()
			client.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, &corev1.NodeList{Items: nodes}, nil
			})
			client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, &corev1.PodList{Items: pods}, nil
			})
			s := NewSSPResourceService(nil, nil)
			s.SetQueueNodeClientResolver(func(SSPQueueItem) (kubernetes.Interface, error) { return client, nil })
			r := ownershipResult(items...)
			quota := r.Quota
			if err := s.FilterQueueUsageNodes(t.Context(), r, freeOnly); err != nil {
				t.Fatal(err)
			}
			want, scope, eligible := []string{"own-full", "own-free"}, queueUsageScopeOwned, 2
			if freeOnly {
				want, scope, eligible = []string{"own-free", "public"}, queueUsageScopeOwnedOrUnassigned, 3
			}
			got := make([]string, 0, len(r.Items))
			for _, item := range r.Items {
				got = append(got, item.HostName)
			}
			if !reflect.DeepEqual(got, want) || r.Scope != scope || r.FreeOnly != freeOnly || r.NodeCount != 4 || r.Ownership.EligibleNodes != eligible || r.Quota != quota || !r.IsQueryComplete() {
				t.Fatalf("nodes=%v result=%+v", got, r)
			}
		})
	}
}

func TestQueueUsageDefaultOwnershipFailureRetainsQuota(t *testing.T) {
	for _, missingUID := range []bool{false, true} {
		t.Run(fmt.Sprint(missingUID), func(t *testing.T) {
			client := fake.NewClientset()
			client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("denied"))
			})
			s := NewSSPResourceService(nil, nil)
			s.SetQueueNodeClientResolver(func(SSPQueueItem) (kubernetes.Interface, error) { return client, nil })
			r := ownershipResult(ownershipUsage("unverified"))
			if missingUID {
				r.Queue.UID = ""
			}
			err := s.FilterQueueUsageNodes(t.Context(), r, false)
			if err == nil || r.Quota == nil || len(r.Items) != 0 || r.Scope != queueUsageScopeOwned || r.FreeOnly || r.OwnershipError == "" || r.IsQueryComplete() {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
}

func TestQueueOwnershipExcludesOtherFragmentsAndUnknownEvidence(t *testing.T) {
	names := []string{"own", "public", "other", "reserved", "mixed", "unknown-workload", "unknown-allocation", "missing", "wrong-acn", "wrong-vc", "same-name-new-uid", "duplicate-acn"}
	items := []SSPQueueNodeUsageItem{}
	nodes := []corev1.Node{}
	for _, name := range names {
		items = append(items, ownershipUsage(name))
		if name != "missing" {
			nodes = append(nodes, ownershipNode(name))
		}
	}
	nodes[3].Labels[nodeQueueUIDLabelKey] = "reserved-other"
	nodes[7].Labels[queueOwnershipACNLabel] = "another-acn"
	nodes[8].Labels[queueOwnershipVCLabel] = "other-vc"
	duplicate := ownershipNode("alias")
	duplicate.Labels[queueOwnershipACNLabel] = "acn-duplicate-acn"
	nodes = append(nodes, duplicate)
	items[6].CPUAllocated = "1"
	pods := []corev1.Pod{ownershipPod("own", "ssp-self-uid"), ownershipPod("other", "other-uid"), ownershipPod("mixed", "self-uid"), ownershipPod("mixed", "other-uid"), ownershipPod("unknown-workload", ""), ownershipPod("same-name-new-uid", "another-generation")}
	pods[len(pods)-1].Labels[nodeQueueNameLabelKey] = "queue-self" // UID must outrank a matching name.
	r := ownershipResult(items...)
	classifyQueueOwnership(r, nodes, pods)
	want := []string{queueNodeOwned, queueNodeUnassigned, queueNodeOther, queueNodeOther, queueNodeOther, queueNodeUnknown, queueNodeUnknown, queueNodeUnknown, queueNodeUnknown, queueNodeUnknown, queueNodeOther, queueNodeUnknown}
	for i, item := range r.Items {
		if item.Ownership != want[i] {
			t.Fatalf("%s: %s want %s", item.HostName, item.Ownership, want[i])
		}
	}
	if r.Ownership.OwnedNodes != 1 || r.Ownership.UnassignedNodes != 1 || r.Ownership.OtherNodes != 4 || r.Ownership.UnknownNodes != 6 {
		t.Fatal(r.Ownership)
	}
	r.FilterFreeNodes()
	if len(r.Items) != 2 || r.Items[0].HostName != "own" || r.Items[1].HostName != "public" || r.NodeCount != len(names) || r.Scope != queueUsageScopeOwnedOrUnassigned || r.IsQueryComplete() {
		t.Fatalf("result=%+v", r)
	}
	if r.Quota.Resources[0].Unallocated != "0" {
		t.Fatal("ownership filter changed quota")
	}
}

func TestQueueNodeReturnsToPublicOnlyAfterOwnerAndAllocationRelease(t *testing.T) {
	for _, test := range []struct {
		name                         string
		phase                        corev1.PodPhase
		deleting, deleted, allocated bool
		want                         string
	}{
		{"other still running", corev1.PodRunning, false, false, true, queueNodeOther},
		{"other terminating", corev1.PodRunning, true, false, true, queueNodeOther},
		{"other bound pending", corev1.PodPending, false, false, true, queueNodeOther},
		{"finished but allocation not cleared", corev1.PodSucceeded, false, false, true, queueNodeUnknown},
		{"last pod gone and allocation released", "", false, true, false, queueNodeUnassigned},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := ownershipUsage("node")
			if test.allocated {
				item.CPUAllocated = "8"
				item.MemoryAllocated = "16GiB"
				item.AcceleratorAllocated = "4"
				item.AcceleratorFree = 4
			}
			pod := ownershipPod("node", "other-uid")
			pod.Status.Phase = test.phase
			if test.deleting {
				now := metav1.Now()
				pod.DeletionTimestamp = &now
			}
			pods := []corev1.Pod{pod}
			if test.deleted {
				pods = nil
			}
			r := ownershipResult(item)
			classifyQueueOwnership(r, []corev1.Node{ownershipNode("node")}, pods)
			if r.Items[0].Ownership != test.want {
				t.Fatal(r.Items[0])
			}
			r.FilterFreeNodes()
			if (len(r.Items) == 1) != (test.want == queueNodeUnassigned) {
				t.Fatal(r.Items)
			}
		})
	}
}

func TestQueueOwnershipFilterNeedsCompleteSnapshotAndPreservesQuota(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint(denied), func(t *testing.T) {
			node := ownershipNode("owned")
			client := fake.NewClientset(&node)
			if denied {
				client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("denied"))
				})
			}
			s := NewSSPResourceService(nil, nil)
			s.SetQueueNodeClientResolver(func(SSPQueueItem) (kubernetes.Interface, error) { return client, nil })
			r := ownershipResult(ownershipUsage("owned"))
			quota := r.Quota
			err := s.FilterQueueFreeNodes(t.Context(), r)
			if r.Quota != quota || !r.FreeOnly {
				t.Fatal(r)
			}
			if denied {
				if err == nil || len(r.Items) != 0 || r.OwnershipError == "" || r.IsQueryComplete() {
					t.Fatalf("result=%+v err=%v", r, err)
				}
			} else if err != nil || len(r.Items) != 1 || r.Items[0].Ownership != queueNodeUnassigned || !r.IsQueryComplete() {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
	r := ownershipResult(ownershipUsage("unverified"))
	r.FilterFreeNodes()
	if len(r.Items) != 0 || r.OwnershipError == "" || r.IsQueryComplete() {
		t.Fatal("unverified pool was treated as public")
	}
}

func TestQueueOwnershipPaginationRejectsPartialOrChangingScans(t *testing.T) {
	for _, mode := range []string{"complete", "repeated", "remaining without cursor", "changed version", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			ctx := t.Context()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			items, err := readQueueOwnershipPages(ctx, "active", func(ctx context.Context, options metav1.ListOptions) ([]int, metav1.ListMeta, error) {
				calls++
				if options.Limit != 500 || options.FieldSelector != "active" {
					t.Fatal(options)
				}
				meta := metav1.ListMeta{ResourceVersion: "one"}
				if calls == 1 {
					meta.Continue = "opaque"
				} else if options.Continue != "opaque" {
					t.Fatal(options)
				}
				switch mode {
				case "repeated":
					meta.Continue = "opaque"
				case "remaining without cursor":
					one := int64(1)
					meta.Continue = ""
					meta.RemainingItemCount = &one
				case "changed version":
					if calls > 1 {
						meta.ResourceVersion = "two"
					}
				}
				return []int{calls}, meta, nil
			})
			if mode == "complete" {
				if err != nil || !reflect.DeepEqual(items, []int{1, 2}) {
					t.Fatal(items, err)
				}
			} else if err == nil || items != nil {
				t.Fatal(items, err)
			}
			if mode == "cancelled" && (!errors.Is(err, context.Canceled) || calls != 0) {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestQueueOwnershipPlatformFallbackReadsAllPodPages(t *testing.T) {
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/virtualClusters/vc-test/api/v1/") || r.URL.Query().Get("limit") != "500" {
			t.Errorf("unscoped query %s", r.URL)
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/nodes") {
			fmt.Fprint(w, `{"items":[{"metadata":{"name":"node","labels":{"resource.compute.sensecore.cn/acn-uid":"acn-node","resource.compute.sensecore.cn/vc-uid":"vc-id"}}}]}`)
			return
		}
		if r.URL.Query().Get("fieldSelector") != "status.phase!=Succeeded,status.phase!=Failed" {
			t.Error(r.URL.Query())
		}
		if r.URL.Query().Get("continue") == "" {
			fmt.Fprint(w, `{"metadata":{"continue":"next","resourceVersion":"123"},"items":[]}`)
			return
		}
		fmt.Fprint(w, `{"metadata":{"resourceVersion":"123"},"items":[{"metadata":{"name":"other","namespace":"apps","labels":{"resource.compute.sensecore.cn/queue-uid":"other-uid"}},"spec":{"nodeName":"node"},"status":{"phase":"Running"}}]}`)
	})
	r := ownershipResult(ownershipUsage("node"))
	if err := NewSSPResourceService(nil, client).FilterQueueFreeNodes(t.Context(), r); err != nil || r.Ownership.OtherNodes != 1 || len(r.Items) != 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestQueueOwnershipLegacyReferencesAndSystemPods(t *testing.T) {
	for _, test := range []struct {
		name, namespace, reference, want string
		gpu                              bool
	}{
		{"volcano reference", "apps", "ssp-self-uid", queueNodeOwned, false},
		{"different volcano reference", "apps", "ssp-other-uid", queueNodeOther, false},
		{"unattributed app", "apps", "", queueNodeUnknown, false},
		{"system infrastructure", "kube-system", "", queueNodeUnassigned, false},
		{"unattributed system gpu", "kube-system", "", queueNodeUnknown, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pod := ownershipPod("node", "")
			pod.Namespace = test.namespace
			pod.Annotations = map[string]string{"scheduling.volcano.sh/queue-name": test.reference}
			if test.gpu {
				pod.Spec.Containers = []corev1.Container{{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceName(metaxGPUResourceName): resource.MustParse("1")}}}}
			}
			r := ownershipResult(ownershipUsage("node"))
			classifyQueueOwnership(r, []corev1.Node{ownershipNode("node")}, []corev1.Pod{pod})
			if r.Items[0].Ownership != test.want {
				t.Fatal(r.Items[0])
			}
		})
	}
}
