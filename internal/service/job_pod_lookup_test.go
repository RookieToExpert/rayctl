package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"rayctl/internal/platform"
)

func TestATEEmptyPodIndexRecoversLiveHostPodAndLogs(t *testing.T) {
	for _, test := range []struct {
		name        string
		indexStatus int
		hostName    string
		resolveNS   bool
		empty       bool
	}{
		{name: "empty index", indexStatus: http.StatusOK},
		{name: "forbidden index", indexStatus: http.StatusForbidden},
		{name: "translated host name", indexStatus: http.StatusOK, hostName: "translated-master-0"},
		{name: "resolve host namespace", indexStatus: http.StatusOK, resolveNS: true},
		{name: "missing Pod is explicit", indexStatus: http.StatusOK, empty: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var current, previous atomic.Int32
			pod := ateLookupHostPod("demo-master-0", "job-uid")
			if test.hostName != "" {
				pod.Name = test.hostName
			}
			pod.Spec = corev1.PodSpec{NodeName: "host", Containers: []corev1.Container{{Name: "main", Image: "registry.test/image:v1", VolumeMounts: []corev1.VolumeMount{{Name: "shm", MountPath: "/dev/shm"}}}}, Volumes: []corev1.Volume{{Name: "shm", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}}
			pod.Status = corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "main", Ready: true, RestartCount: 1, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}}}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasPrefix(r.URL.Path, "/ecp/v1/kubernetes/") && strings.HasSuffix(r.URL.Path, "/jobs/demo"):
					fmt.Fprint(w, `{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","metadata":{"name":"demo","namespace":"default","uid":"job-uid"},"status":{"state":{"phase":"Running"}}}`)
				case strings.HasPrefix(r.URL.Path, "/ecp/v1/kubernetes/") && strings.HasSuffix(r.URL.Path, "/pods"):
					if test.indexStatus != http.StatusOK {
						http.Error(w, "index forbidden", test.indexStatus)
						return
					}
					json.NewEncoder(w).Encode(corev1.PodList{Items: []corev1.Pod{}})
				case r.URL.Path == "/api/v1/namespaces":
					if r.URL.Query().Get("labelSelector") != clusterVirtualNamespaceLabelKey+"=vc-test" {
						t.Error("namespace lookup escaped its VC", r.URL)
					}
					json.NewEncoder(w).Encode(corev1.NamespaceList{Items: []corev1.Namespace{{ObjectMeta: metav1.ObjectMeta{Name: "hc-ns", Labels: map[string]string{clusterLogicalNamespaceLabelKey: "default"}}}}})
				case r.URL.Path == "/api/v1/namespaces/hc-ns/pods":
					rows := []corev1.Pod{}
					if !test.empty {
						rows = append(rows, pod)
					}
					json.NewEncoder(w).Encode(corev1.PodList{Items: rows})
				case r.URL.Path == "/api/v1/namespaces/hc-ns/pods/"+pod.Name:
					json.NewEncoder(w).Encode(pod)
				case r.URL.Path == "/api/v1/nodes/host":
					json.NewEncoder(w).Encode(corev1.Node{Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{Architecture: "arm64"}}})
				case r.URL.Path == "/api/v1/namespaces/hc-ns/pods/"+pod.Name+"/log":
					if r.URL.Query().Get("container") != "main" {
						t.Error("container missing from logs request", r.URL)
					}
					w.Header().Set("Content-Type", "text/plain")
					if r.URL.Query().Get("previous") == "true" {
						previous.Add(1)
						fmt.Fprintln(w, "previous OOM log")
					} else {
						current.Add(1)
						fmt.Fprintln(w, "live host log")
					}
				default:
					t.Errorf("unexpected request %s", r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			client := ateLookupPlatformClient(t, server.URL)
			s := NewJobService(clientset, nil, client)
			identity := jobIdentity{Name: "demo", Namespace: "default", HostNamespace: "hc-ns", VClusterName: "vc-test", VClusterUID: "test", ProfileName: "test"}
			if test.resolveNS {
				identity.HostNamespace = ""
			}
			r, err := s.getJobViaPlatformByIdentity(WithPodEvidenceOptions(context.Background(), true, ""), identity, "demo", 0, time.Now(), true)
			if err != nil {
				t.Fatal(err)
			}
			if test.empty {
				if r.PodEvidence == nil || len(r.PodEvidence.Warnings) == 0 || len(r.Diagnosis) == 0 || current.Load() != 0 || previous.Load() != 0 {
					t.Fatalf("missing Pod silently accepted: diagnosis=%v evidence=%+v", r.Diagnosis, r.PodEvidence)
				}
				return
			}
			if r.InspectPod != "demo-master-0" || len(r.Nodes) != 1 || len(r.Pods) != 1 || r.Pods[0].Namespace != "default" {
				t.Fatalf("missing live Pod: inspect=%q nodes=%v pods=%+v", r.InspectPod, r.Nodes, r.Pods)
			}
			if r.PodEvidence == nil || r.PodEvidence.Total != 1 || len(r.PodEvidence.Pods) != 1 || current.Load() != 1 || previous.Load() != 1 {
				t.Fatalf("evidence=%+v current=%d previous=%d", r.PodEvidence, current.Load(), previous.Load())
			}
			container := r.PodEvidence.Pods[0].Containers[0]
			state, _ := container.State.(map[string]any)
			if state["running"] == nil || container.LastState == nil || container.LastState.Reason != "OOMKilled" || len(container.Mounts) != 1 || len(container.Logs.Current) != 1 {
				t.Fatalf("incomplete container evidence: %+v", container)
			}
		})
	}
}

func TestATEHostPodRecoveryPagesAndRejectsOtherIdentities(t *testing.T) {
	identity := jobIdentity{Name: "demo", Namespace: "default", HostNamespace: "hc-ns", UID: "job-uid", VClusterUID: "test"}
	valid := ateLookupHostPod("demo-master-0", identity.UID)
	delete(valid.Labels, "volcano.sh/job-name")
	delete(valid.Annotations, "vcluster.loft.sh/owner-references")
	valid.OwnerReferences = []metav1.OwnerReference{{Kind: "Job", Name: identity.Name, UID: types.UID(identity.UID)}}
	old := ateLookupHostPod("old-master-0", "previous-job-uid")
	otherNS := ateLookupHostPod("other-ns-master-0", identity.UID)
	otherNS.Annotations["vcluster.loft.sh/object-namespace"] = "another-vc-namespace"
	otherVC := ateLookupHostPod("other-vc-master-0", identity.UID)
	otherVC.Labels["vcluster.loft.sh/vcluster-name"] = "vc-other"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/namespaces/hc-ns/pods" || r.URL.Query().Get("limit") != "500" {
			t.Error("unscoped/unbounded Pod lookup", r.URL)
		}
		page := corev1.PodList{Items: []corev1.Pod{}}
		if r.URL.Query().Get("labelSelector") == "" {
			if r.URL.Query().Get("continue") == "" {
				page.Items = []corev1.Pod{old, otherNS, otherVC}
				page.Continue = "next"
			} else {
				page.Items = []corev1.Pod{valid}
			}
		}
		json.NewEncoder(w).Encode(page)
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	s := NewJobService(client, nil, nil)
	pods, warnings, err := s.recoverATEJobPods(context.Background(), &identity, nil)
	if err != nil || len(pods) != 1 || pods[0].Name != valid.Name || string(pods[0].UID) != "virtual-"+valid.Name || pods[0].Namespace != "default" || calls.Load() != 3 {
		t.Fatalf("pods=%+v calls=%d warnings=%v err=%v", pods, calls.Load(), warnings, err)
	}
}

func TestATEHostPodRecoveryRejectsRepeatedCursor(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(corev1.PodList{ListMeta: metav1.ListMeta{Continue: "same"}})
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	s := NewJobService(client, nil, nil)
	identity := jobIdentity{Name: "demo", UID: "job-uid", Namespace: "default", HostNamespace: "hc-ns"}
	pods, warnings, err := s.recoverATEJobPods(context.Background(), &identity, nil)
	if err != nil || len(pods) != 0 || calls.Load() != 2 || !strings.Contains(strings.Join(warnings, " "), "分页游标重复") {
		t.Fatalf("pods=%v calls=%d warnings=%v err=%v", pods, calls.Load(), warnings, err)
	}
}

func TestATEHostPodRecoveryHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	s := NewJobService(client, nil, nil)
	go func() {
		identity := jobIdentity{Name: "demo", UID: "job-uid", Namespace: "default", HostNamespace: "hc-ns"}
		_, _, err := s.recoverATEJobPods(ctx, &identity, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("HC lookup did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HC lookup ignored cancellation")
	}
}

func ateLookupHostPod(name, jobUID string) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "hc-ns", UID: types.UID("host-" + name),
		Labels: map[string]string{"volcano.sh/job-name": "demo", "vcluster.loft.sh/vcluster-name": "vc-test"},
		Annotations: map[string]string{
			"vcluster.loft.sh/object-name": name, "vcluster.loft.sh/object-namespace": "default",
			"vcluster.loft.sh/object-uid":       "virtual-" + name,
			"vcluster.loft.sh/owner-references": fmt.Sprintf(`[{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","name":"demo","uid":%q}]`, jobUID),
		},
	}}
}

func ateLookupPlatformClient(t *testing.T, endpoint string) *platform.VirtualClusterClient {
	t.Helper()
	path := filepath.Join(t.TempDir(), "platform.json")
	data, err := json.Marshal(map[string]any{"current_profile": "test", "profiles": map[string]any{"test": map[string]any{"access_key": "ak", "secret_key": "sk", "kubernetes_base_url": endpoint, "base_url": endpoint}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	return client
}
