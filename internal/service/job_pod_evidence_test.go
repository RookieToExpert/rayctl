package service

import (
	"context"
	"encoding/json"
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
	"k8s.io/client-go/kubernetes/fake"
	"rayctl/internal/platform"
)

func TestATEPlatformDetailUsesVCLogsAndIncludesFailedReasonAndMounts(t *testing.T) {
	var current, previous atomic.Int32
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "demo-worker-0", Namespace: "virtual-ns"}, Spec: corev1.PodSpec{NodeName: "host", Containers: []corev1.Container{{Name: "worker", Image: "registry.test/p/image:v1", VolumeMounts: []corev1.VolumeMount{{Name: "shm", MountPath: "/dev/shm"}}}}, Volumes: []corev1.Volume{{Name: "shm", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}}}}, Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted", Message: "memory pressure", ContainerStatuses: []corev1.ContainerStatus{{Name: "worker", RestartCount: 1, LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ecp/v1/kubernetes/virtualClusters/vc-test/") && !strings.HasPrefix(r.URL.Path, "/ecp/v1/clusters/vc-test/") {
			t.Error("wrong VC path", r.URL)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/jobs/demo"):
			fmt.Fprint(w, `{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","metadata":{"name":"demo","namespace":"virtual-ns","uid":"uid"},"status":{"state":{"phase":"Failed"}}}`)
		case strings.HasSuffix(r.URL.Path, "/pods"):
			json.NewEncoder(w).Encode(corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}, Items: []corev1.Pod{pod}})
		case strings.HasSuffix(r.URL.Path, "/nodes/host"):
			json.NewEncoder(w).Encode(corev1.Node{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Node"}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{Architecture: "arm64"}}})
		case strings.HasSuffix(r.URL.Path, "/pods/demo-worker-0/log"):
			if r.URL.Query().Get("container") != "worker" {
				t.Error("container lost", r.URL)
			}
			w.Header().Set("Content-Type", "text/plain")
			if r.URL.Query().Get("previous") == "true" {
				previous.Add(1)
				fmt.Fprintln(w, "previous OOM log")
			} else {
				current.Add(1)
				fmt.Fprintln(w, "current log")
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "platform.json")
	data, _ := json.Marshal(map[string]any{"current_profile": "test", "profiles": map[string]any{"test": map[string]any{"access_key": "ak", "secret_key": "sk", "kubernetes_base_url": server.URL, "base_url": server.URL}}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	s := NewJobService(fake.NewSimpleClientset(), nil, client)
	r, err := s.getJobViaPlatformByIdentity(WithPodEvidenceOptions(context.Background(), true, ""), jobIdentity{Name: "demo", Namespace: "virtual-ns", HostNamespace: "different-hc-ns", VClusterName: "vc-test", ProfileName: "test"}, "demo", 0, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if r.PodEvidence == nil || len(r.PodEvidence.Pods) != 1 || current.Load() != 1 || previous.Load() != 1 {
		t.Fatalf("evidence=%+v current=%d previous=%d", r.PodEvidence, current.Load(), previous.Load())
	}
	p := r.PodEvidence.Pods[0]
	if p.Reason != "Evicted" || p.Message != "memory pressure" || len(p.Containers[0].Mounts) != 1 || p.Containers[0].LastState.Reason != "OOMKilled" || r.Pods[0].Reason != "Evicted" {
		t.Fatalf("pod=%+v", p)
	}
}

func TestFailedPodReasonKeepsStatusReasonAndMessage(t *testing.T) {
	pod := corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted", Message: "memory pressure"}}
	if got := podFailureReason(pod); !strings.Contains(got, "Evicted") || !strings.Contains(got, "memory pressure") {
		t.Fatal(got)
	}
}

func TestATEHostPodMappingChecksLogicalNamespaceAndUID(t *testing.T) {
	virtual := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "ns", UID: "virtual-uid"}}
	host := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "hc-ns", Annotations: map[string]string{"vcluster.loft.sh/namespace": "ns", "vcluster.loft.sh/object-uid": "virtual-uid"}}}
	if !ateHostPodMatches(host, virtual) {
		t.Fatal("valid mirror rejected")
	}
	host.Annotations["vcluster.loft.sh/object-uid"] = "other-uid"
	if ateHostPodMatches(host, virtual) {
		t.Fatal("different generation accepted")
	}
	host.Annotations["vcluster.loft.sh/object-uid"] = "virtual-uid"
	host.Annotations["vcluster.loft.sh/namespace"] = "other-ns"
	if ateHostPodMatches(host, virtual) {
		t.Fatal("other namespace accepted")
	}
}
