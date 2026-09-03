package output

import (
	"strings"
	"testing"

	"rayctl/internal/service"
)

func TestJobResourceSpecRowsMergesEqualTaskSpecs(t *testing.T) {
	rows := jobResourceSpecRows([]service.JobResourceSpecItem{
		{Task: "master", Replicas: 1, CPU: "224", Memory: "640Gi", Accelerator: "8", Model: "C550", MachineType: "x2ls.ri.i70"},
		{Task: "worker", Replicas: 3, CPU: "224", Memory: "640Gi", Accelerator: "8", Model: "C550", MachineType: "x2ls.ri.i70"},
	})

	if len(rows) != 1 || rows[0][0] != "SPEC / NODE" || !strings.Contains(rows[0][1], "8 C550") {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestJobResourceSpecRowsKeepsHeterogeneousTasksSeparate(t *testing.T) {
	rows := jobResourceSpecRows([]service.JobResourceSpecItem{
		{Task: "master", Replicas: 1, CPU: "8", Memory: "32Gi"},
		{Task: "worker", Replicas: 4, CPU: "144", Memory: "1920Gi", Accelerator: "8", Model: "module-910b-8"},
	})

	if len(rows) != 2 || rows[0][0] != "SPEC master×1" || rows[1][0] != "SPEC worker×4" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestPrintECPJobListUsesSingleCompactTable(t *testing.T) {
	text := captureTableOutput(t, func() {
		PrintECPJobList(&service.JobClusterListResult{Items: []service.JobClusterItem{
			{JobName: "job-a", Status: "Running", ClusterName: "vc-a", Submitter: "user-a", CreatedAt: "2026-09-03 12:00:00"},
		}})
	})
	for _, expected := range []string{"NAME", "STATE", "VC", "CREATOR", "CREATED", "job-a", "本次筛选共 1 条"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("ECP list output does not contain %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "活跃任务数量") || strings.Count(text, "┌") != 1 {
		t.Fatalf("ECP list output is not a single compact table:\n%s", text)
	}
}
