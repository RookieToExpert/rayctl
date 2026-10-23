package output

import (
	"bytes"
	"encoding/json"
	"rayctl/internal/service"
	"strings"
	"testing"
)

func TestPodLocateJSON(t *testing.T) {
	session := BeginJSON()
	defer EndJSON()
	PrintPodLocateResult(&service.PodLocateResult{HostPod: "pod", WorkloadName: "workload", WorkloadType: "AID", Warnings: []string{"platform offline"}})
	var buf bytes.Buffer
	if err := session.Write(&buf, false, 1, nil); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(buf.Bytes()) || !strings.Contains(buf.String(), `"host_pod": "pod"`) || !strings.Contains(buf.String(), "platform offline") {
		t.Fatal(buf.String())
	}
}

func TestPodLocateTableShowsTerminatingStatusAndRawPhase(t *testing.T) {
	t.Setenv("COLUMNS", "160")
	body := captureTableOutput(t, func() {
		PrintPodLocateResult(&service.PodLocateResult{
			HostPod: "deleting-pod", Status: "Terminating", Phase: "Running", Ready: true, DeletingFor: "29m11s",
		})
	})
	rows := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		columns := strings.Split(line, "│")
		if len(columns) == 4 {
			rows[strings.TrimSpace(columns[1])] = strings.TrimSpace(columns[2])
		}
	}
	for field, want := range map[string]string{
		"STATUS": "Terminating", "PHASE": "Running", "READY": "true", "DELETING FOR": "29m11s",
	} {
		if rows[field] != want {
			t.Fatalf("%s = %q, want %q:\n%s", field, rows[field], want, body)
		}
	}
	if strings.Index(body, "STATUS") > strings.Index(body, "PHASE") {
		t.Fatalf("lifecycle status should precede the raw phase:\n%s", body)
	}
}

func TestPodLocateJSONPreservesTerminatingStatusAndRawPhase(t *testing.T) {
	session := BeginJSON()
	defer EndJSON()
	PrintPodLocateResult(&service.PodLocateResult{HostPod: "deleting-pod", Status: "Terminating", Phase: "Running", Ready: true})
	var buf bytes.Buffer
	if err := session.Write(&buf, false, 1, nil); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status string `json:"status"`
		Phase  string `json:"phase"`
		Ready  bool   `json:"ready"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatal(err, buf.String())
	}
	if payload.Status != "Terminating" || payload.Phase != "Running" || !payload.Ready {
		t.Fatalf("deleting pod lifecycle status or Kubernetes state was lost: %s", buf.String())
	}
}
