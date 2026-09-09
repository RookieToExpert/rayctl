package cmd

import "testing"

func TestAFSCreateCommandDefaults(t *testing.T) {
	command := newAFSCreateCmd()
	checks := map[string]string{
		"size":       "1TB",
		"class":      "OCEANSTOR",
		"years":      "10",
		"auto-renew": "true",
	}
	for name, want := range checks {
		flag := command.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("missing --%s flag", name)
		}
		if flag.DefValue != want {
			t.Fatalf("--%s default = %q, want %q", name, flag.DefValue, want)
		}
	}
	for _, name := range []string{"zone", "display-name", "dry-run", "yes"} {
		if command.Flags().Lookup(name) == nil {
			t.Fatalf("missing --%s flag", name)
		}
	}
}
