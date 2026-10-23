package output

import (
	"strings"
	"testing"
	"time"

	"rayctl/internal/podevidence"
)

func TestPodEvidenceShowsHistoricalRestartSeparatelyFromCurrentPod(t *testing.T) {
	count := int64(1)
	text := captureTableOutput(t, func() {
		printPodEvidence(&podevidence.Result{
			Total: 1, Restarts24h: &count, HistoryPods: []string{"demo-0"},
			LastRestart24h: &podevidence.RestartObservation{ObservedAt: time.Date(2026, 10, 18, 15, 36, 37, 0, time.UTC), Pod: "demo-0", Container: "main", UID: "old-uid", Reason: "Error"},
		})
	})
	for _, want := range []string{"current Pod cumulative restarts 0", "Observed restarts in 24h (1 inspected Pods): 1", "Last observed restart in 24h: 2026-10-18 23:36:37 (UTC+8)", "demo-0/main, reason Error, Pod UID old-uid"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}
