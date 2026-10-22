package output

import (
	"bytes"
	"encoding/json"
	"rayctl/internal/podevidence"
	"rayctl/internal/service"
	"strings"
	"testing"
)

func TestATEBasicJSONKeepsExistingPodSummaryShape(t *testing.T) {
	session := BeginJSON()
	defer EndJSON()
	PrintJobDetail(&service.JobGetResult{Name: "demo", Pods: []service.JobPodItem{{Name: "worker", Phase: "Running"}}}, false, false)
	var buffer bytes.Buffer
	if err := session.Write(&buffer, false, 1, nil); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if _, ok := value["pod_evidence"]; ok {
		t.Fatal("basic output introduced uncollected evidence")
	}
	pods, ok := value["pods"].([]any)
	if !ok || len(pods) != 1 || pods[0].(map[string]any)["name"] != "worker" {
		t.Fatal(buffer.String())
	}
}

func TestATEFailedReasonAndDetailedEvidenceAreVisible(t *testing.T) {
	r := &service.JobGetResult{Name: "demo", Status: "Failed", Terminal: true, Pods: []service.JobPodItem{{Name: "worker", Phase: "Failed", Reason: "Evicted", Message: "memory pressure"}}, PodEvidence: &podevidence.Result{Total: 1, Pods: []podevidence.Pod{{Name: "worker", Phase: "Failed", Reason: "Evicted", Containers: []podevidence.Container{{Name: "app", Mounts: []podevidence.Mount{{Name: "data", Path: "/data", Type: "PVC", Source: "afs-data"}}, Logs: podevidence.Logs{Previous: []string{"previous failure log"}}}}}}}}
	for _, long := range []bool{false, true} {
		text := captureTableOutput(t, func() { PrintJobDetail(r, long, false) })
		if !strings.Contains(text, "Evicted") || !strings.Contains(text, "memory pressure") {
			t.Fatal(text)
		}
		if long {
			for _, want := range []string{"POD EVIDENCE", "previous failure log", "/data", "afs-data", "status.reason: Evicted"} {
				if !strings.Contains(text, want) {
					t.Fatal(want, text)
				}
			}
		}
	}
}
