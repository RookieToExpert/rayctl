package cmd

import "testing"

func TestNormalizeNodeIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "IPv4", input: "10.140.214.222", want: "host-10-140-214-222"},
		{name: "IPv4 with spaces", input: " 10.12.138.28 ", want: "host-10-12-138-28"},
		{name: "host name", input: "host-10-140-214-222", want: "host-10-140-214-222"},
		{name: "custom node name", input: "worker-a", want: "worker-a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeNodeIdentifier(tt.input); got != tt.want {
				t.Fatalf("normalizeNodeIdentifier(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
