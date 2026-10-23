package output

import (
	"bytes"
	"encoding/json"
	"rayctl/internal/service"
	"strings"
	"testing"
)

func TestNodeWorkloadsJSON(t *testing.T) {
	session := BeginJSON()
	defer EndJSON()
	PrintNodeDescribe(&service.NodeDescribe{Hostname: "node", Workloads: []service.NodeWorkload{{Pod: "pod", Type: "OTHER", Source: "k8s", AcceleratorRequests: map[string]string{"nvidia.com/gpu": "1"}}}, Warnings: []string{"platform timeout"}}, false, 0, false)
	var buf bytes.Buffer
	if err := session.Write(&buf, false, 1, nil); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Fatal(buf.String())
	}
	for _, text := range []string{`"workloads"`, `"source": "k8s"`, `"accelerator_requests"`, `"warnings"`, `platform timeout`} {
		if !strings.Contains(buf.String(), text) {
			t.Fatalf("missing %s: %s", text, buf.String())
		}
	}
}

func TestNodeWorkloadsTable(t *testing.T) {
	t.Setenv("COLUMNS", "240")
	items := []service.NodeWorkload{{Namespace: "ns", Pod: "pod", Type: "OTHER", Source: "k8s", Workload: "agent", Phase: "Pending", Status: "ContainerCreating", VC: "vc-name", Workspace: "ws-name", Creator: "alice", AcceleratorRequests: map[string]string{"nvidia.com/gpu": "2"}}}
	text := captureTableOutput(t, func() { printNodeWorkloads(items, false) })
	for _, value := range []string{"ns/pod", "OTHER", "agent", "STATUS", "ContainerCreating"} {
		if !strings.Contains(text, value) {
			t.Fatalf("missing %q: %s", value, text)
		}
	}
	for _, value := range []string{"VC", "WORKSPACE", "CREATOR", "ACCEL REQ", "SOURCE", "vc-name", "ws-name", "alice", "nvidia.com/gpu", "k8s", "Pending"} {
		if strings.Contains(text, value) {
			t.Fatalf("unexpected %q in compact table: %s", value, text)
		}
	}
	longText := captureTableOutput(t, func() { printNodeWorkloads(items, true) })
	for _, value := range []string{"VC", "WORKSPACE", "CREATOR", "ACCEL REQ", "SOURCE", "vc-name", "ws-name", "alice", "nvidia.com/gpu=2", "k8s", "ContainerCreating"} {
		if !strings.Contains(longText, value) {
			t.Fatalf("missing %q in long table: %s", value, longText)
		}
	}
}

func TestNodeWorkloadsSummaryHidesVCUnlessLong(t *testing.T) {
	t.Setenv("COLUMNS", "240")
	details := &service.NodeDescribe{Hostname: "node", VClusterName: "vc-name", Workloads: []service.NodeWorkload{{Namespace: "ns", Pod: "pod", Type: "OTHER", Status: "Running"}}}
	compact := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil, false) })
	if strings.Contains(compact, "VC") || strings.Contains(compact, "vc-name") {
		t.Fatalf("VC should be hidden in compact output: %s", compact)
	}
	long := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil, true) })
	if !strings.Contains(long, "VC") || !strings.Contains(long, "vc-name") {
		t.Fatalf("VC should appear in long output: %s", long)
	}
}
