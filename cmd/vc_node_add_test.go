package cmd

import "testing"

func TestVCNodeAddCommand(t *testing.T) {
	cmd := newVCNodeAddCmd()
	if cmd.Use != "add <vc-name-or-uid> <node-name-or-ip-or-uid>..." {
		t.Fatalf("Use = %q", cmd.Use)
	}
	for _, name := range []string{"dry-run", "long", "yes"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("missing --%s flag", name)
		}
	}
}
