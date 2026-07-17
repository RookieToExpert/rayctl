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
