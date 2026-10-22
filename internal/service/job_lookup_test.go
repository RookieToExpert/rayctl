package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"rayctl/internal/platform"
)

func lookupTestService(t *testing.T, handler http.HandlerFunc, withPlatform bool) *JobService {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	config := &rest.Config{Host: server.URL, QPS: 100, Burst: 100}
	kube, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	var vc *platform.VirtualClusterClient
	if withPlatform {
		vc = ateLookupPlatformClient(t, server.URL)
	}
	return NewJobService(kube, dyn, vc)
}

func writeLookupMetadata(w http.ResponseWriter, items []metav1.PartialObjectMetadata, next string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metav1.PartialObjectMetadataList{
		TypeMeta: metav1.TypeMeta{APIVersion: "meta.k8s.io/v1", Kind: "PartialObjectMetadataList"},
		ListMeta: metav1.ListMeta{Continue: next}, Items: items,
	})
}

func TestATEMissingLookupCompletesOnceWithoutWaitingForDeadline(t *testing.T) {
	for _, withPlatform := range []bool{false, true} {
		t.Run(fmt.Sprint("platform=", withPlatform), func(t *testing.T) {
			var jobs, podPages atomic.Int32
			s := lookupTestService(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/compute/ecp/v1/subscriptions/-/resourceGroups/-/regions/-/virtualClusters":
					fmt.Fprint(w, `{"virtual_clusters":[]}`)
				case "/apis/batch.volcano.sh/v1alpha1/jobs", "/api/v1/pods":
					if r.URL.Query().Get("fieldSelector") != "" || (r.URL.Query().Get("labelSelector") != "" && r.URL.Query().Get("labelSelector") != atePodLookupSelector) {
						json.NewEncoder(w).Encode(corev1.PodList{Items: []corev1.Pod{}})
						return
					}
					if !strings.HasPrefix(r.Header.Get("Accept"), "application/vnd.kubernetes.protobuf;as=PartialObjectMetadataList") || r.URL.Query().Get("limit") != "5000" {
						t.Error("lookup downloaded unbounded full objects", r.URL, r.Header.Get("Accept"))
					}
					if strings.HasSuffix(r.URL.Path, "/jobs") {
						jobs.Add(1)
					} else {
						podPages.Add(1)
					}
					if r.URL.Query().Get("continue") == "" {
						writeLookupMetadata(w, nil, "page-2")
					} else {
						writeLookupMetadata(w, nil, "")
					}
				default:
					t.Error("unexpected request", r.URL)
					http.NotFound(w, r)
				}
			}, withPlatform)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := s.GetJob(ctx, "certainly-missing-ate")
			var missing *jobNotFoundError
			if !errors.As(err, &missing) || !strings.Contains(err.Error(), "不存在") || ctx.Err() != nil {
				t.Fatalf("err=%v parent=%v", err, ctx.Err())
			}
			if jobs.Load() != 2 || podPages.Load() != 2 {
				t.Fatalf("repeated or incomplete scan: job pages=%d pod pages=%d", jobs.Load(), podPages.Load())
			}
		})
	}
}

func TestATELookupErrorsCannotBecomeNotFound(t *testing.T) {
	for _, source := range []string{"pods", "jobs", "inventory", "vc", "pods404", "vc404"} {
		t.Run(source, func(t *testing.T) {
			s := lookupTestService(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				inventory := strings.HasPrefix(r.URL.Path, "/compute/")
				vc := strings.HasPrefix(r.URL.Path, "/ecp/")
				fail := (source == "inventory" && inventory) || (strings.HasPrefix(source, "vc") && vc) || (strings.HasPrefix(source, "pods") && r.URL.Path == "/api/v1/pods") || (source == "jobs" && r.URL.Path == "/apis/batch.volcano.sh/v1alpha1/jobs")
				if fail {
					if strings.HasSuffix(source, "404") {
						w.WriteHeader(http.StatusNotFound)
						fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","message":"Pod API endpoint missing","code":404}`)
						return
					}
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"lookup forbidden","code":403}`)
					return
				}
				if inventory {
					fmt.Fprint(w, `{"virtual_clusters":[{"uid":"test","name":"vc-test"}]}`)
				} else if vc {
					fmt.Fprint(w, `{"kind":"JobList","items":[]}`)
				} else if r.URL.Query().Get("fieldSelector") != "" || (r.URL.Query().Get("labelSelector") != "" && r.URL.Query().Get("labelSelector") != atePodLookupSelector) {
					json.NewEncoder(w).Encode(corev1.PodList{Items: []corev1.Pod{}})
				} else {
					writeLookupMetadata(w, nil, "")
				}
			}, true)
			_, err := s.GetJob(context.Background(), "missing-job")
			var missing *jobNotFoundError
			var incomplete *jobLookupError
			want := "forbidden"
			if strings.HasSuffix(source, "404") {
				want = "endpoint missing"
			}
			if err == nil || errors.As(err, &missing) || !errors.As(err, &incomplete) || !strings.Contains(strings.ToLower(err.Error()), want) {
				t.Fatalf("real %s error lost: %v", source, err)
			}
		})
	}
}

func TestATELookupCancellationInterruptsMetadataScan(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint("deadline=", deadline), func(t *testing.T) {
			started := make(chan struct{}, 1)
			s := lookupTestService(t, func(w http.ResponseWriter, r *http.Request) {
				select {
				case started <- struct{}{}:
				default:
				}
				<-r.Context().Done()
			}, false)
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
				want = context.DeadlineExceeded
			}
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := s.GetJob(ctx, "missing"); done <- err }()
			<-started
			if !deadline {
				cancel()
			}
			select {
			case err := <-done:
				var missing *jobNotFoundError
				if !errors.Is(err, want) || errors.As(err, &missing) {
					t.Fatalf("cancellation misclassified: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancellation did not interrupt lookup")
			}
		})
	}
}

func TestATEJobMetadataMatchesAliasesAndPreservesAmbiguity(t *testing.T) {
	for _, id := range []string{"translated-a", "logical-a", "logical-uid-a", "host-uid-a", "logical-", "absent"} {
		t.Run(id, func(t *testing.T) {
			var gets atomic.Int32
			s := lookupTestService(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/namespaces/") {
					gets.Add(1)
					if !strings.HasSuffix(r.URL.Path, "/ns-a/jobs/translated-a") {
						t.Error("wrong matching job", r.URL)
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(&unstructured.Unstructured{Object: map[string]any{"apiVersion": "batch.volcano.sh/v1alpha1", "kind": "Job", "metadata": map[string]any{"name": "translated-a", "namespace": "ns-a"}, "spec": map[string]any{"minAvailable": int64(3)}}})
					return
				}
				items := []metav1.PartialObjectMetadata{}
				for _, name := range []string{"a", "b"} {
					items = append(items, metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "translated-" + name, Namespace: "ns-" + name, UID: types.UID("host-uid-" + name), Annotations: map[string]string{"vcluster.loft.sh/object-name": "logical-" + name, "vcluster.loft.sh/object-uid": "logical-uid-" + name}}})
				}
				writeLookupMetadata(w, items, "")
			}, false)
			job, err := s.findJob(context.Background(), id)
			var ambiguous *ambiguousJobMatchError
			var missing *jobNotFoundError
			switch id {
			case "logical-":
				if !errors.As(err, &ambiguous) || len(ambiguous.Matches) != 2 || gets.Load() != 0 {
					t.Fatalf("ambiguity lost: %v gets=%d", err, gets.Load())
				}
			case "absent":
				if !errors.As(err, &missing) || gets.Load() != 0 {
					t.Fatalf("missing=%v gets=%d", err, gets.Load())
				}
			default:
				if err != nil || desiredReplicaTotal(job) != 3 || gets.Load() != 1 {
					t.Fatalf("match=%v err=%v gets=%d", job, err, gets.Load())
				}
			}
		})
	}
}

func TestATEPodMetadataLookupHydratesOnlyMatchedPod(t *testing.T) {
	for _, id := range []string{"demo-master-0", "demo-master", "job-uid", "demo", "absent"} {
		t.Run(id, func(t *testing.T) {
			var gets atomic.Int32
			s := lookupTestService(t, func(w http.ResponseWriter, r *http.Request) {
				pod := ateLookupHostPod("demo-master-0", "job-uid")
				pod.Spec.NodeName = "node-a"
				if strings.Contains(r.URL.Path, "/namespaces/") {
					gets.Add(1)
					if r.URL.Path != "/api/v1/namespaces/hc-ns/pods/demo-master-0" {
						t.Error("wrong Pod fetched", r.URL)
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(pod)
					return
				}
				if r.URL.Query().Get("fieldSelector") != "" || (r.URL.Query().Get("labelSelector") != "" && r.URL.Query().Get("labelSelector") != atePodLookupSelector) {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(corev1.PodList{Items: []corev1.Pod{}})
					return
				}
				writeLookupMetadata(w, []metav1.PartialObjectMetadata{{ObjectMeta: pod.ObjectMeta}}, "")
			}, false)
			pod, err := s.findHostPodForIdentifier(context.Background(), id)
			if id == "absent" {
				var missing *jobNotFoundError
				if !errors.As(err, &missing) || gets.Load() != 0 {
					t.Fatalf("missing lookup=%v gets=%d", err, gets.Load())
				}
			} else if err != nil || pod.Spec.NodeName != "node-a" || gets.Load() != 1 {
				t.Fatalf("pod=%v err=%v gets=%d", pod, err, gets.Load())
			}
		})
	}
}

func TestATELookupRejectsRepeatedPaginationAndExpiredSnapshot(t *testing.T) {
	for _, resource := range []string{"pods", "jobs"} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/expired=%v", resource, expired), func(t *testing.T) {
				s := lookupTestService(t, func(w http.ResponseWriter, r *http.Request) {
					if expired && r.URL.Query().Get("continue") != "" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusGone)
						fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Expired","message":"snapshot expired","code":410}`)
						return
					}
					writeLookupMetadata(w, nil, "same-cursor")
				}, false)
				var err error
				if resource == "jobs" {
					_, err = s.listJobLookupMetadata(context.Background())
				} else {
					_, err = s.matchingHostPodMetadata(context.Background(), "missing")
				}
				if err == nil || (!strings.Contains(err.Error(), "cursor repeated") && !strings.Contains(err.Error(), "snapshot expired")) {
					t.Fatalf("incomplete pagination accepted: %v", err)
				}
			})
		}
	}
}

func TestATEPlatformLookupPreservesNamespaceAndVCAmbiguity(t *testing.T) {
	for _, acrossVC := range []bool{false, true} {
		t.Run(fmt.Sprint("acrossVC=", acrossVC), func(t *testing.T) {
			s := lookupTestService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("filter") != `name="demo"` || strings.Contains(r.URL.Path, "/namespaces/") {
					t.Error("lookup lost namespace scope", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				items := []map[string]any{{"metadata": map[string]any{"name": "demo", "namespace": "ns-a", "uid": r.URL.Path + "-a"}}}
				if !acrossVC {
					items = append(items, map[string]any{"metadata": map[string]any{"name": "demo", "namespace": "ns-b", "uid": "uid-b"}})
				}
				json.NewEncoder(w).Encode(map[string]any{"kind": "JobList", "items": items})
			}, true)
			s.clientset = nil // Host namespace enrichment is independent of ambiguity.
			vcs := []platform.VirtualCluster{{UID: "one", ProfileName: "test"}}
			if acrossVC {
				vcs = append(vcs, platform.VirtualCluster{UID: "two", ProfileName: "test"})
			}
			_, err := s.findPlatformJobIdentityInClusters(context.Background(), "demo", vcs)
			var ambiguous *ambiguousJobMatchError
			if !errors.As(err, &ambiguous) || len(ambiguous.Matches) != 2 {
				t.Fatalf("ambiguity lost: %v", err)
			}
		})
	}
}

func TestATEUIDLookupRejectsRepeatedPodCursor(t *testing.T) {
	s := lookupTestService(t, func(w http.ResponseWriter, r *http.Request) {
		writeLookupMetadata(w, nil, "same-cursor")
	}, false)
	_, err := s.findPodIdentityByUID(context.Background(), "e8360839-e5cb-4dc7-8e7a-90c050176105")
	if err == nil || !strings.Contains(err.Error(), "cursor repeated") {
		t.Fatalf("UID lookup accepted incomplete pagination: %v", err)
	}
}
