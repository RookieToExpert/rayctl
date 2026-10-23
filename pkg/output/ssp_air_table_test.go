package output

import (
	"fmt"
	"strings"
	"testing"

	"rayctl/internal/podevidence"
	"rayctl/internal/service"
)

func TestPrintAIRJobDetailDistinguishesGroupsAndConfiguredInstances(t *testing.T) {
	result := &service.SSPAIRJobItem{
		Name: "infer-job", ReadyReplicas: 1, Replicas: 1, DesiredReplicas: 1, GroupSize: 8, TotalInstances: 8,
	}
	text := captureTableOutput(t, func() { PrintSSPAIRJobDetail(result, false) })
	for field, want := range map[string]string{
		"GROUPS READY/TOTAL": "1 / 1",
		"DESIRED GROUPS":     "1",
		"INSTANCES/GROUP":    "8",
		"TOTAL INSTANCES":    "8",
	} {
		if got := airSummaryValue(text, field); got != want {
			t.Fatalf("%s = %q, want %q:\n%s", field, got, want, text)
		}
	}
}

func TestPrintAIRJobDefaultShowsEightInstancesAndSeparatesAuxiliaryPod(t *testing.T) {
	t.Setenv("COLUMNS", "400")
	result := &service.SSPAIRJobItem{
		Name: "infer-job", ReadyReplicas: 1, Replicas: 1, DesiredReplicas: 1, GroupSize: 8, TotalInstances: 8, WorkerTotal: 9,
		AuxiliaryWorkers: []service.SSPAIRWorkerItem{{Name: "metrics-pod", Role: "metrics", Phase: "Running", HostIP: "10.140.215.99", PodIP: "10.0.0.99"}},
	}
	for index := range 8 {
		role := "worker"
		if index == 0 {
			role = "leader"
		}
		result.Workers = append(result.Workers, service.SSPAIRWorkerItem{
			Name: fmt.Sprintf("infer-job-0-%d", index), Group: "0", Role: role, Phase: "Running",
			Node: fmt.Sprintf("node-%d", index), HostIP: fmt.Sprintf("10.140.215.%d", index+10), PodIP: fmt.Sprintf("10.0.0.%d", index+10), Restarts: index,
			StartedAt: "2026-10-09 10:00:00", LastStarted: "2026-10-09 11:00:00",
		})
	}
	text := captureTableOutput(t, func() { PrintSSPAIRJobDetail(result, false) })
	instancesStart := strings.Index(text, "INSTANCES: showing 8 (configured total 8)")
	auxiliaryStart := strings.Index(text, "AUXILIARY PODS: showing 1")
	if instancesStart < 0 || auxiliaryStart < instancesStart {
		t.Fatalf("missing separate compute and auxiliary counts:\n%s", text)
	}
	instancesText := text[instancesStart:auxiliaryStart]
	auxiliaryText := text[auxiliaryStart:]
	for _, header := range []string{"INSTANCE", "GROUP", "ROLE", "PHASE", "NODE", "HOST IP", "POD IP", "RESTARTS"} {
		if !strings.Contains(instancesText, header) {
			t.Fatalf("default instance table lacks %q:\n%s", header, instancesText)
		}
	}
	for _, worker := range result.Workers {
		for _, value := range []string{worker.Name, worker.Node, worker.HostIP, worker.PodIP, worker.Role} {
			if !strings.Contains(instancesText, value) {
				t.Fatalf("default instance table lacks %q:\n%s", value, instancesText)
			}
		}
		if strings.Contains(auxiliaryText, worker.Name) {
			t.Fatalf("compute instance %q appears in auxiliary table:\n%s", worker.Name, auxiliaryText)
		}
	}
	if strings.Contains(instancesText, "metrics-pod") || !strings.Contains(auxiliaryText, "metrics-pod") {
		t.Fatalf("metrics Pod must appear only in auxiliary table:\n%s", text)
	}
	for _, omitted := range []string{"showing 8 of 9", "LAST STARTED", "2026-10-09 10:00:00"} {
		if strings.Contains(text, omitted) {
			t.Fatalf("default output unexpectedly contains %q:\n%s", omitted, text)
		}
	}
}

func TestPrintAIRJobLeaderServiceIPExplainsUnassignedAddress(t *testing.T) {
	result := &service.SSPAIRJobItem{Workers: []service.SSPAIRWorkerItem{{Name: "leader-pod", HostIP: "10.140.215.10", PodIP: "10.0.0.10"}}}
	text := captureTableOutput(t, func() { PrintSSPAIRJobDetail(result, false) })
	if got := airSummaryValue(text, "LEADER SERVICE IP"); got != "- (not assigned)" {
		t.Fatalf("leader service IP = %q, want unassigned explanation:\n%s", got, text)
	}
	if strings.Contains(text, "INTERNAL IP") {
		t.Fatalf("obsolete INTERNAL IP label remains:\n%s", text)
	}
	result.InternalIP = "10.43.1.2"
	text = captureTableOutput(t, func() { PrintSSPAIRJobDetail(result, false) })
	if got := airSummaryValue(text, "LEADER SERVICE IP"); got != result.InternalIP {
		t.Fatalf("leader service IP = %q, want %q:\n%s", got, result.InternalIP, text)
	}
}

func TestPrintAIRJobDefaultKeepsInstanceAndNodeNamesOnOneLine(t *testing.T) {
	t.Setenv("COLUMNS", "100")
	worker := service.SSPAIRWorkerItem{
		Name: "infer-911-glm-8n-dcp4-5-0-worker-6", Group: "0", Role: "worker", Phase: "Running",
		Node: "ecp-a3-superpod1-d809", HostIP: "10.140.215.56", PodIP: "10.0.0.56",
	}
	auxiliary := service.SSPAIRWorkerItem{
		Name: "infer-911-glm-8n-dcp4-5-metrics-exporter-0-1234567890123456789", Role: "AUXILIARY", Phase: "Running",
	}
	text := captureTableOutput(t, func() {
		PrintSSPAIRJobDetail(&service.SSPAIRJobItem{Workers: []service.SSPAIRWorkerItem{worker}, AuxiliaryWorkers: []service.SSPAIRWorkerItem{auxiliary}}, false)
	})
	instanceFound, auxiliaryFound := false, false
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, worker.Name) && strings.Contains(line, worker.Node) && strings.Contains(line, worker.HostIP) && strings.Contains(line, worker.PodIP) {
			instanceFound = true
		}
		if strings.Contains(line, auxiliary.Name) && strings.Contains(line, auxiliary.Role) {
			auxiliaryFound = true
		}
	}
	if !instanceFound || !auxiliaryFound {
		t.Fatalf("default table truncates or wraps instance or auxiliary details:\n%s", text)
	}
}

func TestPrintAIRJobLongKeepsResourceVolumeAndPodEvidenceDetails(t *testing.T) {
	t.Setenv("COLUMNS", "400")
	result := &service.SSPAIRJobItem{
		GroupSize: 8, TotalInstances: 8,
		Resource:    service.SSPAIRResourceItem{CPU: "240", Memory: "960GiB", Image: "registry.example/inference:v1"},
		DNATRules:   []service.SSPAIRDNATItem{{External: "1.2.3.4:80", Internal: "10.0.0.1:8000", Protocol: "TCP", Gateway: "nat-gateway"}},
		Volumes:     []service.SSPAIRVolumeItem{{Type: "NFS", Name: "model-volume", MountPath: "/models", Endpoint: "nfs.example/models"}},
		Workers:     []service.SSPAIRWorkerItem{{Name: "leader-pod", StartedAt: "2026-10-09 10:00:00", LastStarted: "2026-10-09 11:00:00"}},
		PodEvidence: &podevidence.Result{Total: 1, Omitted: 1, Restarts: 2},
	}
	shortText := captureTableOutput(t, func() { PrintSSPAIRJobDetail(result, false) })
	longText := captureTableOutput(t, func() { PrintSSPAIRJobDetail(result, true) })
	for _, value := range []string{"CPU=240", "registry.example/inference:v1", "model-volume", "nat-gateway", "STARTED", "LAST STARTED", "2026-10-09 10:00:00", "2026-10-09 11:00:00", "POD EVIDENCE:"} {
		if strings.Contains(shortText, value) {
			t.Fatalf("default output unexpectedly contains long detail %q:\n%s", value, shortText)
		}
		if !strings.Contains(longText, value) {
			t.Fatalf("long output lacks %q:\n%s", value, longText)
		}
	}
}

func TestPrintAIRJobListShowsGroupSizeAndConfiguredTotal(t *testing.T) {
	t.Setenv("COLUMNS", "400")
	result := &service.SSPAIRJobListResult{Items: []service.SSPAIRJobItem{{
		Name: "infer-job", ReadyReplicas: 1, Replicas: 1, DesiredReplicas: 1, GroupSize: 8, TotalInstances: 8,
	}}}
	text := captureTableOutput(t, func() { PrintSSPAIRJobList(result) })
	var headers, values []string
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "│") {
			continue
		}
		cells := airTableCells(line)
		if headers == nil {
			headers = cells
		} else {
			values = cells
		}
	}
	for column, want := range map[string]string{"READY": "1/1 ×8"} {
		found := false
		for index, header := range headers {
			if header == column {
				found = index < len(values) && values[index] == want
			}
		}
		if !found {
			t.Fatalf("list missing %s = %q:\n%s", column, want, text)
		}
	}
}

func TestPrintAIRJobUnknownGroupSizeKeepsInstanceCountsUnknown(t *testing.T) {
	text := captureTableOutput(t, func() {
		PrintSSPAIRJobDetail(&service.SSPAIRJobItem{DesiredReplicas: 1, WorkerTotal: 9}, false)
	})
	for _, field := range []string{"INSTANCES/GROUP", "TOTAL INSTANCES"} {
		if got := airSummaryValue(text, field); got != "-" {
			t.Fatalf("%s = %q, want unknown:\n%s", field, got, text)
		}
	}
	if !strings.Contains(text, "INSTANCES: showing 0 (configured total -)") {
		t.Fatalf("unknown configured total was inferred from auxiliary Pod count:\n%s", text)
	}
	text = captureTableOutput(t, func() {
		PrintSSPAIRJobDetail(&service.SSPAIRJobItem{TotalInstances: 8}, false)
	})
	if got := airSummaryValue(text, "TOTAL INSTANCES"); got != "8" {
		t.Fatalf("API configured total = %q, want 8 despite unknown group size:\n%s", got, text)
	}
}

func airSummaryValue(text, field string) string {
	for _, line := range strings.Split(text, "\n") {
		cells := airTableCells(line)
		if len(cells) == 2 && cells[0] == field {
			return cells[1]
		}
	}
	return ""
}

func airTableCells(line string) []string {
	parts := strings.Split(line, "│")
	if len(parts) < 3 {
		return nil
	}
	cells := parts[1 : len(parts)-1]
	for index := range cells {
		cells[index] = strings.TrimSpace(cells[index])
	}
	return cells
}
