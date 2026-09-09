package cmd

import "testing"

func TestNodeDetailCommandIsGetOnly(t *testing.T) {
	node := newNodeCmd()
	get, _, err := node.Find([]string{"get"})
	if err != nil || get.Name() != "get" {
		t.Fatalf("node get unavailable: command=%v err=%v", get, err)
	}
	for _, legacy := range []string{"describe", "check"} {
		command, args, err := node.Find([]string{legacy})
		if err == nil && command != node && len(args) == 0 {
			t.Fatalf("legacy command %q is still registered as %q", legacy, command.Name())
		}
	}
}
