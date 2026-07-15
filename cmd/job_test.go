package cmd

import "testing"

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
