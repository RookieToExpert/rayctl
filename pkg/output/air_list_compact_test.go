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

func TestAIRJobListWidePreservesCreatedTimestamp(t *testing.T) {
	const created = "2026-10-23 09:12:34"
	const name = "infer-glm-53-flash-w8a8c16-long-name"
	for _, columns := range []string{"80", "120", "200"} {
		t.Run(columns, func(t *testing.T) {
			t.Setenv("COLUMNS", columns)
			result := &service.SSPAIRJobListResult{Items: []service.SSPAIRJobItem{{
				Name: name, State: "RUNNING", Workspace: "ws-d-a3-trustrouter",
				Queue: "queue-d-reserved-a3-trustrouter", Creator: "creator", CreatedAt: created,
			}}}
			text := captureTableOutput(t, func() { PrintSSPAIRJobList(result, true) })
			found := false
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, name) && strings.Contains(line, created) {
					found = true
				}
			}
			if !found || strings.Count(text, created) != 1 {
				t.Fatalf("timestamp must remain intact on its job row at %s columns:\n%s", columns, text)
			}
		})
	}
}
