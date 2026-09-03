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
