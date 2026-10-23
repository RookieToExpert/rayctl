package output

import (
	"rayctl/internal/service"
	"strings"
	"testing"
)

func TestAIRJobListCompactAndWide(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	r := &service.SSPAIRJobListResult{Items: []service.SSPAIRJobItem{{Name: "infer-model", State: "Running", Workspace: "ws-project", Queue: "queue-project", ReadyReplicas: 1, Replicas: 1, GroupSize: 2, TotalInstances: 2, Creator: "creator", CreatedAt: "2026-10-23 09:00:00"}}}
	compact := captureTableOutput(t, func() { PrintSSPAIRJobList(r) })
	if !strings.Contains(compact, "1/1 ×2") || strings.Contains(compact, "QUEUE") || strings.Contains(compact, "TOTAL INSTANCES") {
		t.Fatal(compact)
	}
	for _, line := range strings.Split(compact, "\n") {
		if textWidth(line) > 120 {
			t.Fatalf("too wide: %s", line)
		}
	}
	wide := captureTableOutput(t, func() { PrintSSPAIRJobList(r, true) })
	if !strings.Contains(wide, "queue-project") || !strings.Contains(wide, "CREATED") {
		t.Fatal(wide)
	}
}
