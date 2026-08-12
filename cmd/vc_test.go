package cmd

import "testing"

func TestVCCommandContainsMergedClusterCommands(t *testing.T) {
	command := newVCCmd()
	for _, name := range []string{"get", "node", "set"} {
		if child, _, err := command.Find([]string{name}); err != nil || child == command {
			t.Fatalf("vc subcommand %q is missing: %v", name, err)
		}
	}

	getCommand, _, err := command.Find([]string{"get"})
	if err != nil {
		t.Fatalf("find vc get: %v", err)
	}
	if getCommand.Flags().Lookup("platform-only") == nil {
		t.Fatal("vc get --platform-only flag is missing")
	}
	if err := getCommand.Args(getCommand, []string{"vc-a", "vc-b", "vc-c"}); err != nil {
		t.Fatalf("vc get should accept multiple identifiers: %v", err)
	}
	if getCommand.Use != "get [vc-name-or-uid...]" {
		t.Fatalf("vc get use = %q", getCommand.Use)
	}
}

func TestLegacyClusterCommandIsHidden(t *testing.T) {
	command := newClusterCmd()
	if !command.Hidden {
		t.Fatal("legacy cluster command should be hidden")
	}
	if command.Deprecated == "" {
		t.Fatal("legacy cluster command should point users to vc")
	}
	for _, name := range []string{"get", "set"} {
		if child, _, err := command.Find([]string{name}); err != nil || child == command {
			t.Fatalf("legacy cluster subcommand %q is missing: %v", name, err)
		}
	}
}
