package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"rayctl/internal/service"
)

func TestNormalizeJobGetIdentifier(t *testing.T) {
	tests := map[string]string{
		"WYL":                                  "wyl",
		" Job-ABC-Worker-0 ":                   "job-abc-worker-0",
		"7253824A-36F2-457F-87D3-881BB8BF51EA": "7253824a-36f2-457f-87d3-881bb8bf51ea",
		"already-lowercase-job":                "already-lowercase-job",
	}
	for input, want := range tests {
		if got := normalizeJobGetIdentifier(input); got != want {
			t.Fatalf("normalizeJobGetIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestJobResultsContainSSPTrainingJob(t *testing.T) {
	if !jobResultsContainSSPTrainingJob([]*service.JobGetResult{{WorkloadType: service.SSPWorkloadTypeTrainingJob}}) {
		t.Fatal("SSP TrainingJob result was not detected")
	}
	if jobResultsContainSSPTrainingJob([]*service.JobGetResult{{WorkloadType: ""}}) {
		t.Fatal("ordinary ECP result was classified as SSP TrainingJob")
	}
}

func TestFormatJobGetTimeoutError(t *testing.T) {
	if defaultJobGetTimeout != 10*time.Second {
		t.Fatalf("defaultJobGetTimeout = %s, want 10s", defaultJobGetTimeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	err := formatJobGetError(ctx, "example-job", defaultJobGetTimeout, context.DeadlineExceeded)
	for _, fragment := range []string{"example-job", "10s", "kubeconfig", "D/PT"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("timeout error %q does not contain %q", err, fragment)
		}
	}
}
