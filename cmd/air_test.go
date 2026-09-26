package cmd

import "testing"

func TestAIRJobListStateFlag(t *testing.T) {
	cmd := newAIRJobListCmd()
	state := cmd.Flags().Lookup("state")
	if state == nil || state.Shorthand != "s" {
		t.Fatalf("AIR job list state flag = %#v", state)
	}
}
