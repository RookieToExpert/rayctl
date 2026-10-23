package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"rayctl/internal/platform"
)

func TestVCWorkloadsCombineSSPAndNativeECPJobs(t *testing.T) {
	var ecpCalls, sspQueueCalls atomic.Int32
	client := workloadTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/compute/ecp/") && strings.HasSuffix(r.URL.Path, "/virtualClusters/vc-demo"):
			fmt.Fprint(w, `{"name":"vc-demo","uid":"vc-uid","tenant_id":"sub","region":"cn-pj-01"}`)
		case r.URL.Path == "/compute/ssp/v1/subscriptions/-/resourceGroups/-/regions/-/clusters":
			fmt.Fprint(w, `{"clusters":[{"name":"ssp-demo","uid":"ssp-uid","id":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/ssp-demo","properties":{"source":{"name":"vc-demo","uid":"vc-uid"}}},{"name":"ssp-foreign","uid":"ssp-foreign","id":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/clusters/ssp-foreign","properties":{"source":{"name":"vc-demo","uid":"wrong-uid"}}}]}`)
		case strings.HasSuffix(r.URL.Path, "/clusters/ssp-demo/queues"):
			sspQueueCalls.Add(1)
			fmt.Fprint(w, `{"queues":[{"name":"ssp-q","uid":"q","workspace_name":"ws"}]}`)
		case strings.HasSuffix(r.URL.Path, "/ssp-q/workloads"):
			fmt.Fprint(w, `{"workloads":[{"name":"ssp-job","uid":"ssp-job-uid","type":"trainingJob","state":"Running","workspace":{"name":"ws"},"create_time":"2026-10-08T06:00:00Z"}]}`)
		case strings.HasSuffix(r.URL.Path, "/apis/batch.volcano.sh/v1alpha1/jobs"):
			ecpCalls.Add(1)
			fmt.Fprint(w, `{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"JobList","items":[{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","metadata":{"name":"native-job","uid":"ecp-job-uid","namespace":"custom","creationTimestamp":"2026-10-08T07:00:00Z","labels":{"resource.compute.sensecore.cn/workload-type":"vcJob","lepton.sensetime.com/submitter":"alice"}},"spec":{"queue":"native-q","priorityClassName":"normal"},"status":{"state":{"phase":"Running"}}},{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","metadata":{"name":"ssp-job","uid":"k8s-ssp-job-uid","labels":{"resource.compute.sensecore.cn/workload-type":"training-job"},"creationTimestamp":"2026-10-08T06:00:00Z"},"status":{"state":{"phase":"Running"}}}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	result, err := NewVCService(client).ListWorkloads(context.Background(), "vc-demo", SSPQueueWorkloadQuery{})
	if err != nil || result == nil || len(result.Items) != 2 || result.Items[0].Type != "vcJob" || result.Items[1].Type != "trainingJob" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if result.ClusterName != "vc-demo" || result.ClusterUID != "vc-uid" || result.ProfileName != "test" || result.Items[0].Namespace != "custom" || result.Items[0].Creator != "alice" {
		t.Fatalf("result metadata = %#v", result)
	}
	if ecpCalls.Load() != 1 || sspQueueCalls.Load() != 1 {
		t.Fatalf("API calls ECP/SSP = %d/%d", ecpCalls.Load(), sspQueueCalls.Load())
	}
}

func TestVCWorkloadsReturnNativeJobsWhenSSPDiscoveryFails(t *testing.T) {
	client := workloadTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/compute/ecp/") && strings.HasSuffix(r.URL.Path, "/virtualClusters/vc-demo"):
			fmt.Fprint(w, `{"name":"vc-demo","uid":"uid","tenant_id":"sub","region":"cn-pj-01"}`)
		case r.URL.Path == "/compute/ssp/v1/subscriptions/-/resourceGroups/-/regions/-/clusters":
			http.Error(w, "forbidden", http.StatusForbidden)
		case strings.HasSuffix(r.URL.Path, "/apis/batch.volcano.sh/v1alpha1/jobs"):
			fmt.Fprint(w, `{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"JobList","items":[{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","metadata":{"name":"native-job"}}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	result, err := NewVCService(client).ListWorkloads(context.Background(), "vc-demo", SSPQueueWorkloadQuery{})
	if err == nil || result == nil || len(result.Items) != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "SSP clusters") {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestSSPClusterBelongsToVCUsesScopeAndSourceResource(t *testing.T) {
	const subscriptionUID = "11111111-1111-4111-8111-111111111111"
	const vcUID = "22222222-2222-4222-8222-222222222222"
	cluster := VCDetailResult{Name: "vc-demo", UID: vcUID, Tenant: "test", Subscription: subscriptionUID, Region: "cn-pj-01"}
	candidate := platform.SSPCluster{ProfileName: "test", Subscription: subscriptionUID, Region: "cn-pj-01"}
	candidate.Properties.Source.Name = "vc-demo"
	cases := []struct {
		name string
		uid  string
		id   string
		want bool
	}{
		{"source UID", vcUID, "", true},
		{"wrong UID with matching name", subscriptionUID, "", false},
		{"RID with subscription UUID and VC UID", "", "/subscriptions/" + subscriptionUID + "/resourceGroups/default/regions/cn-pj-01/virtualClusters/" + vcUID, true},
		{"RID with subscription UUID and VC name", "", "/subscriptions/" + subscriptionUID + "/resourceGroups/default/regions/cn-pj-01/virtualClusters/vc-demo", true},
		{"name fallback", "", "", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate.Properties.Source.UID = test.uid
			candidate.Properties.Source.ID = test.id
			if got := sspClusterBelongsToVC(candidate, cluster); got != test.want {
				t.Fatalf("sspClusterBelongsToVC() = %v, want %v", got, test.want)
			}
		})
	}
	candidate.Properties.Source.UID = vcUID
	candidate.ProfileName = "other"
	if sspClusterBelongsToVC(candidate, cluster) {
		t.Fatal("matched a cluster from a different profile")
	}
	candidate.ProfileName = "test"
	candidate.Subscription = "other"
	if sspClusterBelongsToVC(candidate, cluster) {
		t.Fatal("matched a cluster from a different subscription")
	}
	candidate.Subscription = subscriptionUID
	candidate.Region = "cn-pj-03"
	if sspClusterBelongsToVC(candidate, cluster) {
		t.Fatal("matched a cluster from a different region")
	}
}

func TestScopedWorkloadTypeRoutesOnlyRelevantSources(t *testing.T) {
	for _, value := range []string{"vcJob", "vc-job", "ecp-vcjob", "volcano"} {
		if !includesECPWorkloadType(value) || !isECPOnlyWorkloadType(value) || isSSPManagedVCWorkloadType(value) {
			t.Fatalf("native type %q was routed incorrectly", value)
		}
	}
	for _, value := range []string{"", "job", "trainingJob", "training-job"} {
		if !includesECPWorkloadType(value) || isECPOnlyWorkloadType(value) {
			t.Fatalf("generic type %q was routed incorrectly", value)
		}
	}
	for _, value := range []string{"aid", "air", "inferGateway"} {
		if includesECPWorkloadType(value) || !isSSPManagedVCWorkloadType(value) {
			t.Fatalf("SSP type %q was routed incorrectly", value)
		}
	}
}

func TestDCloudVCWorkloadsSkipSSPSource(t *testing.T) {
	client := workloadTestPlatformForEnvironment(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/virtualClusters/demo") {
			fmt.Fprint(w, `{"name":"demo","uid":"uid","tenant_id":"sub","region":"cn-pj-01"}`)
			return
		}
		t.Errorf("unexpected request %s", r.URL.Path)
		http.NotFound(w, r)
	}, platform.ClusterDCloud)
	for _, workloadType := range []string{"", "aid"} {
		result, err := NewVCService(client).ListWorkloads(context.Background(), "demo", SSPQueueWorkloadQuery{Type: workloadType})
		if err == nil || result == nil || len(result.Warnings) != 1 {
			t.Fatalf("type=%q result=%#v error=%v", workloadType, result, err)
		}
		if workloadType == "aid" && !strings.Contains(err.Error(), "not available in DCloud") {
			t.Fatalf("unsupported SSP type error = %v", err)
		}
		if workloadType == "" && strings.Contains(err.Error(), "SSP") {
			t.Fatalf("default DCloud query unexpectedly attempted SSP: %v", err)
		}
	}
}

func TestLocalVCWorkloadJobsRetainCustomNamespaceWithoutSubmitter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/apis/batch.volcano.sh/v1alpha1/jobs" {
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"JobList","items":[{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","metadata":{"name":"native","namespace":"custom","annotations":{"identity.compute.sensecore.cn/user-name":"alice"}}}]}`)
	}))
	defer server.Close()
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	config := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\ncurrent-context: test\nusers:\n- name: test\n  user: {}\n", server.URL)
	if err := os.WriteFile(kubeconfig, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	jobs, err := listLocalVCWorkloadJobs(context.Background(), kubeconfig)
	if err != nil || len(jobs) != 1 || jobs[0].GetNamespace() != "custom" {
		t.Fatalf("jobs=%#v error=%v", jobs, err)
	}
}
