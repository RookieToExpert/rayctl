package cmd

import "testing"

func TestNormalizeQueueWorkloadType(t *testing.T) {
	for input, want := range map[string]string{
		"job": "trainingJob", "aid": "aid", "gw": "inferGateway", "air": "air",
	} {
		if got := normalizeQueueWorkloadType(input); got != want {
			t.Fatalf("normalizeQueueWorkloadType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestQueueWorkloadAlias(t *testing.T) {
	queue := newQueueCmd()
	workload, _, err := queue.Find([]string{"wl"})
	if err != nil || workload == nil || workload.Name() != "workload" {
		t.Fatalf("queue wl lookup = %#v, %v", workload, err)
	}
}
