package cmd

import "testing"

func TestAuthNetworkResourceCommands(t *testing.T) {
	authCmd := newAuthCmd()
	for _, parentName := range []string{"check", "grant", "remove", "roles"} {
		parent, _, err := authCmd.Find([]string{parentName})
		if err != nil || parent == nil || parent.Name() != parentName {
			t.Fatalf("auth %s command not found: %v", parentName, err)
		}
		for _, resourceName := range []string{"vpc", "eip", "natgateway"} {
			child, _, childErr := parent.Find([]string{resourceName})
			if childErr != nil || child == nil || child.Name() != resourceName {
				t.Fatalf("auth %s %s command not found: %v", parentName, resourceName, childErr)
			}
		}
	}
}

func TestAuthSSPCommandsAndEnvironmentFlags(t *testing.T) {
	authCmd := newAuthCmd()
	ssp, _, err := authCmd.Find([]string{"ssp"})
	if err != nil || ssp == nil {
		t.Fatalf("auth ssp command not found: %v", err)
	}
	for _, childName := range []string{"check", "grant"} {
		child, _, childErr := ssp.Find([]string{childName})
		if childErr != nil || child == nil || child.Name() != childName {
			t.Fatalf("auth ssp %s command not found: %v", childName, childErr)
		}
		if child.Flags().Lookup("environment") != nil {
			t.Fatalf("auth ssp %s unexpectedly exposes --environment", childName)
		}
	}

	for _, childName := range []string{"user", "groups"} {
		child, _, childErr := authCmd.Find([]string{"check", childName})
		if childErr != nil || child == nil {
			t.Fatalf("auth check %s command not found: %v", childName, childErr)
		}
		flag := child.Flags().Lookup("environment")
		if flag == nil || flag.Shorthand != "v" {
			t.Fatalf("auth check %s --environment/-v flag missing", childName)
		}
	}
}
