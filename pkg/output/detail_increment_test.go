package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"rayctl/internal/podevidence"
	"rayctl/internal/service"
)

func TestAITLongIsIncrementalAndDeduplicatesPVCs(t *testing.T) {
	r := &service.SSPJobGetResult{Name: "job-one", UID: "uid-extra", Status: "Running", Workspace: "default-workspace", HostNamespace: "host-ns", Nodes: []string{"node-1"}, ImagePullSecrets: []string{"pull-secret-extra"}, PersistentVolumeClaims: []service.VolumeClaimRef{{ClaimName: "claim-unique", Status: "Bound", FrontendVolume: "afs-unique"}}, PodEvidence: &podevidence.Result{Total: 1, Warnings: []string{"采样间隔约 30s，间隔内发生的事件无法被观测"}, Pods: []podevidence.Pod{{Name: "worker", Namespace: "host-ns", Containers: []podevidence.Container{{Name: "app", Logs: podevidence.Logs{Current: []string{}, Previous: []string{" "}}, Mounts: []podevidence.Mount{{Name: "data", Type: "PVC", Source: "claim-unique", Path: "/data", SubPath: "training", ReadOnly: true}, {Name: "vcluster-rewrite-hosts", Type: "ConfigMap", Path: "/etc/hosts"}}}}}}}}
	before, _ := json.Marshal(r)
	short := captureTableOutput(t, func() { PrintSSPJobDetail(r, false) })
	long := captureTableOutput(t, func() { PrintSSPJobDetail(r, true) })
	if !strings.Contains(short, "default-workspace") || strings.Contains(long, "default-workspace") || strings.Contains(long, "REQUIRED NODES") {
		t.Fatalf("summary repeated: %s", long)
	}
	for _, value := range []string{"job-one | status=Running | host namespace=host-ns | nodes=node-1", "uid-extra", "pull-secret-extra", "/data", "training", "true", "afs-unique", "已折叠 1 项"} {
		if !strings.Contains(long, value) {
			t.Fatalf("missing %q: %s", value, long)
		}
	}
	if strings.Count(long, "claim-unique") != 1 || strings.Contains(long, "vcluster-rewrite-hosts") || strings.Contains(long, "tail 30") || strings.Contains(long, "采样间隔约") {
		t.Fatal(long)
	}
	after, _ := json.Marshal(r)
	if !bytes.Equal(before, after) {
		t.Fatal("view mutated full result")
	}
	session := BeginJSON()
	defer EndJSON()
	PrintSSPJobDetail(r, true)
	var buffer bytes.Buffer
	if err := session.Write(&buffer, false, 1, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buffer.String(), "default-workspace") || !strings.Contains(buffer.String(), "vcluster-rewrite-hosts") {
		t.Fatalf("JSON was filtered: %s", buffer.String())
	}
}

func TestMultiObjectLongHasIndependentAnchors(t *testing.T) {
	text := captureTableOutput(t, func() {
		PrintSSPAIDDetail(&service.SSPAIDGetResult{Name: "aid-a", State: "Running", HostNamespace: "ns-a"}, true, false)
		PrintSSPAIDDetail(&service.SSPAIDGetResult{Name: "aid-b", State: "Stopped", HostNamespace: "ns-b"}, true, false)
	})
	if strings.Count(text, "| status=") != 2 || !strings.Contains(text, "aid-a | status=Running") || !strings.Contains(text, "aid-b | status=Stopped") {
		t.Fatal(text)
	}
}

func TestAFSMultiLongHasPerObjectIncrementsAndFullJSON(t *testing.T) {
	results := []*service.AFSCheckResult{{AFSName: "afs-one", State: "RUNNING", Capacity: "100Gi", HostPVs: []string{"pv-one"}}, {AFSName: "afs-two", State: "RUNNING", Capacity: "200Gi", HostPVs: []string{"pv-two"}}}
	text := captureTableOutput(t, func() { PrintAFSCheckSummary(results, true) })
	if strings.Count(text, "| status=") != 2 || !strings.Contains(text, "pv-one") || !strings.Contains(text, "pv-two") || strings.Contains(text, "CAPACITY") {
		t.Fatal(text)
	}
	session := BeginJSON()
	defer EndJSON()
	PrintAFSCheckSummary(results, true)
	var buffer bytes.Buffer
	if err := session.Write(&buffer, true, 2, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buffer.String(), "100Gi") || !strings.Contains(buffer.String(), "pv-two") {
		t.Fatal(buffer.String())
	}
}
