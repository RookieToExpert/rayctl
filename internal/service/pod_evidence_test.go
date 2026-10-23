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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"rayctl/internal/platform"
	"rayctl/internal/podevidence"
)

func restartHistoryPlatformClient(t *testing.T, handler http.HandlerFunc, credentials bool) *platform.VirtualClusterClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	password := ""
	if credentials {
		password = "test-pass"
	}
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","region":"cn-pj-01","base_url":%q,"metrics_base_url":%q,"metrics_username":"test-user","metrics_password":%q}}}`, server.URL, server.URL, password)
	configPath := filepath.Join(t.TempDir(), "platform.json")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	for _, key := range []string{"RAYCTL_METRICS_BASE_URL", "RAYCTL_METRICS_USERNAME", "RAYCTL_METRICS_PASSWORD"} {
		t.Setenv(key, "")
	}
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeRestartHistorySeries(t *testing.T, w http.ResponseWriter, uid, reason string, values []float64, timestamps ...time.Time) {
	t.Helper()
	labels := map[string]string{"pod": "demo-0", "container": "main", "uid": uid, "sco_srm_type": "compute.ssp.v1.workspace"}
	if reason != "" {
		labels["reason"] = reason
	}
	millis := make([]int64, len(timestamps))
	for i, timestamp := range timestamps {
		millis[i] = timestamp.UnixMilli()
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"metric": labels, "values": values, "timestamps": millis}); err != nil {
		t.Error(err)
	}
}

func TestAIDShortDetailFindsRestartBeforePodReplacement(t *testing.T) {
	restartAt := time.Now().Add(-12 * time.Hour).Truncate(time.Second)
	requests := 0
	client := restartHistoryPlatformClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		query := r.URL.Query().Get("match[]")
		if r.URL.Path != "/prometheus/api/v1/export" || !strings.Contains(query, "demo-0") || strings.Contains(query, "memory") {
			t.Errorf("expected a targeted restart query, got %s", r.URL)
			http.NotFound(w, r)
			return
		}
		switch {
		case strings.HasPrefix(query, "kube_pod_container_status_restarts_total"):
			writeRestartHistorySeries(t, w, "old-uid", "", []float64{0, 1, 0}, restartAt.Add(-time.Hour), restartAt, restartAt.Add(time.Hour))
			writeRestartHistorySeries(t, w, "new-uid", "", []float64{0}, restartAt.Add(2*time.Hour))
		case strings.HasPrefix(query, "kube_pod_container_status_last_terminated_reason"):
			writeRestartHistorySeries(t, w, "old-uid", "Error", []float64{1}, restartAt)
		default:
			t.Errorf("unexpected metric: %s", query)
		}
	}, true)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-0", UID: "new-uid", Namespace: "host-ns"},
		Spec:       corev1.PodSpec{NodeName: "node-a"},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "main", Ready: true, RestartCount: 0}},
		},
	}
	kubeClient := fake.NewSimpleClientset(&pod)
	aid := platform.SSPAID{Name: "demo", State: "RUNNING"}
	aid.Properties.Workload.WorkspaceName = "ws-demo"
	result := NewSSPAIDService(kubeClient, client).buildResult(context.Background(), aid, "host-ns", []corev1.Pod{pod}, nil, false)
	if result.Stage != "running" || result.PodEvidence == nil {
		t.Fatalf("missing evidence from short AID detail: %#v", result)
	}
	evidence := result.PodEvidence
	if requests != 2 || evidence.Restarts != 0 || evidence.Restarts24h == nil || *evidence.Restarts24h != 1 {
		t.Fatalf("requests = %d, evidence = %#v", requests, evidence)
	}
	if last := evidence.LastRestart24h; last == nil || last.UID != "old-uid" || last.Reason != "Error" || !last.ObservedAt.Equal(restartAt) {
		t.Fatalf("last restart = %#v; later resets must not replace the restart event", last)
	}
	diagnosis := strings.Join(result.Diagnosis, " ")
	for _, want := range []string{"近 24h 监控观察到重启 1 次", "当前 Pod 累计重启 0 次", "最近一次重启监控观测于", restartAt.In(utcPlus8).Format("2006-01-02 15:04:05"), "原因 Error"} {
		if !strings.Contains(diagnosis, want) {
			t.Errorf("short AID diagnosis %q missing %q", diagnosis, want)
		}
	}
	for _, action := range kubeClient.Actions() {
		if action.GetSubresource() == "log" {
			t.Fatal("short AID detail fetched container logs")
		}
	}
}

func TestRestartHistoryDoesNotTreatBaselinesOrResetsAsRestarts(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	client := restartHistoryPlatformClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Query().Get("match[]"), "kube_pod_container_status_restarts_total") {
			writeRestartHistorySeries(t, w, "old", "", []float64{1, 0}, at, at.Add(time.Minute))
			writeRestartHistorySeries(t, w, "new", "", []float64{0}, at.Add(2*time.Minute))
		}
	}, true)
	evidence := enrichRestartHistory(context.Background(), client, []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "demo-0"}}}, "demo", "aid", "ws-demo", &podevidence.Result{})
	if evidence.Restarts24h == nil || *evidence.Restarts24h != 0 || evidence.LastRestart24h != nil {
		t.Fatalf("baselines and resets became restart events: %#v", evidence)
	}
}

func TestUnavailableRestartHistoryRemainsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name        string
		credentials bool
		status      int
	}{
		{name: "no samples", credentials: true, status: http.StatusOK},
		{name: "request failed", credentials: true, status: http.StatusServiceUnavailable},
		{name: "no credentials", credentials: false, status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := restartHistoryPlatformClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) }, tc.credentials)
			pods := []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "demo-0"}}}
			evidence := enrichRestartHistory(context.Background(), client, pods, "demo", "aid", "ws-demo", &podevidence.Result{})
			if evidence.Restarts24h != nil || evidence.LastRestart24h != nil || len(evidence.Warnings) == 0 {
				t.Fatalf("unavailable history was reported as known: %#v", evidence)
			}
			if diagnosis := aidRestartDiagnosis(pods, evidence); !strings.Contains(diagnosis, "近 24h 重启历史暂不可用") || strings.Contains(diagnosis, "监控观察到重启 0 次") {
				t.Fatalf("misleading diagnosis: %s", diagnosis)
			}
		})
	}
}

func TestAIDRestartDiagnosisSelectsLatestEvidenceAndPreservesExactTime(t *testing.T) {
	finishedAt := time.Date(2026, 10, 18, 23, 36, 14, 0, utcPlus8)
	for _, tc := range []struct {
		name       string
		uid        string
		counter    int64
		observedAt time.Time
		wantTime   time.Time
		wantReason string
		observed   bool
	}{
		{name: "same event uses exact Kubernetes time", uid: "current", counter: 1, observedAt: finishedAt.Add(23 * time.Second), wantTime: finishedAt, wantReason: "OOMKilled"},
		{name: "older metric keeps newer Kubernetes event", uid: "old", counter: 1, observedAt: finishedAt.Add(-time.Hour), wantTime: finishedAt, wantReason: "OOMKilled"},
		{name: "different UID retains newer historical restart", uid: "old", counter: 1, observedAt: finishedAt.Add(time.Hour), wantTime: finishedAt.Add(time.Hour), wantReason: "Error", observed: true},
		{name: "new restart after Kubernetes snapshot", uid: "current", counter: 2, observedAt: finishedAt.Add(time.Minute), wantTime: finishedAt.Add(time.Minute), wantReason: "Error", observed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pods := []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-0", UID: types.UID("current")},
				Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
					Name: "main", RestartCount: 1,
					LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", FinishedAt: metav1.NewTime(finishedAt)}},
				}}},
			}}
			count := int64(1)
			evidence := &podevidence.Result{Restarts24h: &count, LastRestart24h: &podevidence.RestartObservation{
				Pod: "demo-0", Container: "main", UID: tc.uid, ObservedAt: tc.observedAt, RestartCount: tc.counter, Reason: "Error",
			}}
			diagnosis := aidRestartDiagnosis(pods, evidence)
			if !strings.Contains(diagnosis, tc.wantTime.Format("2006-01-02 15:04:05")) || !strings.Contains(diagnosis, "原因 "+tc.wantReason) || strings.Contains(diagnosis, "监控观测于") != tc.observed {
				t.Fatalf("wrong latest restart: %s", diagnosis)
			}
		})
	}
}
