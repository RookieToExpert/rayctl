package cmd

import "testing"

func TestRootUsesAITAndATEOnly(t *testing.T) {
	for _, name := range []string{"ait", "ate"} {
		command, args, err := rootCmd.Find([]string{name})
		if err != nil || command == rootCmd || len(args) != 0 {
			t.Fatalf("root command %q unavailable: command=%v args=%v err=%v", name, command, args, err)
		}
	}
	for _, removed := range []string{"job", "ecp"} {
		command, args, err := rootCmd.Find([]string{removed})
		if err == nil && command != rootCmd && len(args) == 0 {
			t.Fatalf("removed root command %q is still registered", removed)
		}
	}
}

func TestATEExposesGetListAndCreate(t *testing.T) {
	command := newATECmd()
	for _, name := range []string{"get", "list", "create"} {
		child, args, err := command.Find([]string{name})
		if err != nil || child == command || len(args) != 0 {
			t.Fatalf("ate %q unavailable: child=%v args=%v err=%v", name, child, args, err)
		}
	}
}
