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
	for _, fragment := range []string{"example-job", "10s", "平台 API", "HC API", "kubeconfig", "platform profile"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("timeout error %q does not contain %q", err, fragment)
		}
	}
}

func TestJobGetHelpIncludesClusterUsage(t *testing.T) {
	cmd := newJobGetCmd()
	help := strings.Join([]string{cmd.Short, cmd.Long, cmd.Example}, "\n")
	for _, fragment := range []string{
		"按 VC 分区",
		"job get cluster vc-a3-intern-delivery",
		"job get cluster -a pending",
		"--all-status",
	} {
		if !strings.Contains(help, fragment) {
			t.Fatalf("job get help does not contain %q:\n%s", fragment, help)
		}
	}

	clusterCmd, _, err := cmd.Find([]string{"cluster"})
	if err != nil {
		t.Fatalf("find cluster subcommand: %v", err)
	}
	clusterHelp := strings.Join([]string{clusterCmd.Long, clusterCmd.Example}, "\n")
	for _, fragment := range []string{"Running 和 Pending", "--all-status", "当前租户全部 VC"} {
		if !strings.Contains(clusterHelp, fragment) {
			t.Fatalf("job get cluster help does not contain %q:\n%s", fragment, clusterHelp)
		}
	}
}
