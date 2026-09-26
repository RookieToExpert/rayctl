package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"rayctl/internal/platform"
	authsession "rayctl/internal/session"
)

func TestPTReusesMatchingDIDTokenOnly(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "platform.json")
	config := `{"current_profile":"tenant-pt","profiles":{"tenant":{"access_key":"ak","secret_key":"sk","region":"cn-pj-01"},"tenant-pt":{"access_key":"ak","secret_key":"sk","region":"cn-pj-03"},"other":{"access_key":"ak","secret_key":"sk","region":"cn-pj-01"}}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "pt")
	t.Setenv("RAYCTL_BEARER_TOKEN", "")
	client, err := platform.NewVirtualClusterClientFromEnvWithError()
	if err != nil {
		t.Fatal(err)
	}
	store := &authsession.Store{Profiles: map[string]authsession.ProfileSession{
		"tenant": {IDToken: "d-id-token", AccessToken: "d-access-token"},
		"other":  {IDToken: "other-id-token"},
	}}
	t.Setenv("HOME", t.TempDir())
	if err := authsession.Save("", store); err != nil {
		t.Fatal(err)
	}
	if token, source, err := bearerTokenForAuthReadCommand(context.Background(), &cobra.Command{}, client, ""); err != nil || token != "d-access-token" || source != "session/paired-d-access_token" {
		t.Fatalf("PT read token = %q, source = %q, err = %v", token, source, err)
	}
	if token, source, err := bearerTokenForAuthGrantCommand(context.Background(), &cobra.Command{}, client, ""); err != nil || token != "d-access-token" || source != "session/paired-d-access_token" {
		t.Fatalf("PT grant token = %q, source = %q, err = %v", token, source, err)
	}
	if token, source, err := bearerTokenForAuthGrantCommand(context.Background(), &cobra.Command{}, client, "explicit-token"); err != nil || token != "explicit-token" || source != "--bearer-token" {
		t.Fatalf("explicit grant token = %q, source = %q, err = %v", token, source, err)
	}
	if iam, compute, source, err := bearerTokensForRBACGrantCommand(context.Background(), &cobra.Command{}, client, ""); err != nil || iam != "d-access-token" || compute != "d-id-token" || source != "session/paired-d-access_token + session/paired-d-id_token" {
		t.Fatalf("PT RBAC grant tokens = %q, %q, source = %q, err = %v", iam, compute, source, err)
	}
	if token, ok := validIDTokenForProfile(store, client, "tenant-pt"); !ok || token != "d-id-token" {
		t.Fatalf("PT fallback = %q, %v", token, ok)
	}
	store.Profiles["tenant-pt"] = authsession.ProfileSession{IDToken: "pt-id-token", AccessToken: "pt-access-token"}
	if err := authsession.Save("", store); err != nil {
		t.Fatal(err)
	}
	if token, source, err := bearerTokenForAuthGrantCommand(context.Background(), &cobra.Command{}, client, ""); err != nil || token != "pt-access-token" || source != "session/access_token" {
		t.Fatalf("PT own grant token = %q, source = %q, err = %v", token, source, err)
	}
	if token, ok := validIDTokenForProfile(store, client, "tenant-pt"); !ok || token != "pt-id-token" {
		t.Fatalf("PT own token = %q, %v", token, ok)
	}
	if token, ok := validIDTokenForProfile(store, client, "other"); !ok || token != "other-id-token" {
		t.Fatalf("D own token = %q, %v", token, ok)
	}
	delete(store.Profiles, "tenant")
	delete(store.Profiles, "tenant-pt")
	if token, ok := validIDTokenForProfile(store, client, "tenant-pt"); ok || token != "" {
		t.Fatalf("unmatched PT fallback = %q, %v", token, ok)
	}
}

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

func TestGroupGetCommandIsSeparateFromAuth(t *testing.T) {
	group, _, err := rootCmd.Find([]string{"group", "get"})
	if err != nil || group == nil || group.Name() != "get" {
		t.Fatalf("group get command not found: %v", err)
	}
	if group.Flags().Lookup("long") == nil {
		t.Fatal("group get --long flag missing")
	}
	auth, _, err := newAuthCmd().Find([]string{"check", "group"})
	if err != nil || auth == nil || auth.Name() != "groups" {
		t.Fatalf("auth check group alias not found: %v", err)
	}
}

func TestUserAndGroupListCommands(t *testing.T) {
	for _, parent := range []*cobra.Command{newUserCmd(), newGroupCmd()} {
		list, _, err := parent.Find([]string{"list"})
		if err != nil || list == nil || list.Name() != "list" {
			t.Fatalf("%s list command not found: %v", parent.Name(), err)
		}
		if list.Flags().Lookup("limit") == nil || list.Flags().Lookup("long") == nil {
			t.Fatalf("%s list flags missing", parent.Name())
		}
		if err := list.Args(list, []string{"unexpected"}); err == nil {
			t.Fatalf("%s list accepted positional argument", parent.Name())
		}
	}
}

func TestAuthCommandsUseGlobalEnvironmentFlag(t *testing.T) {
	authCmd := newAuthCmd()
	ssp, _, err := authCmd.Find([]string{"ssp"})
	if err != nil || ssp == nil {
		t.Fatalf("auth ssp command not found: %v", err)
	}
	if !ssp.Hidden || ssp.Deprecated == "" {
		t.Fatal("legacy auth ssp should be hidden and deprecated")
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
		if child.Flags().Lookup("environment") != nil {
			t.Fatalf("auth check %s unexpectedly exposes a local environment flag", childName)
		}
	}
	for _, path := range [][]string{{"check", "ws"}, {"grant", "ws"}, {"roles", "ws"}} {
		child, _, childErr := authCmd.Find(path)
		if childErr != nil || child == nil || child.Name() != "ws" {
			t.Fatalf("auth %s ws command not found: %v", path[0], childErr)
		}
		if child.Flags().Lookup("environment") != nil {
			t.Fatalf("auth %s ws unexpectedly exposes a local environment flag", path[0])
		}
	}
	check, _, _ := authCmd.Find([]string{"check", "ws"})
	if err := check.Args(check, []string{"ws-demo"}); err != nil {
		t.Fatalf("auth check ws rejected workspace: %v", err)
	}
	grant, _, _ := authCmd.Find([]string{"grant", "ws"})
	for _, flag := range []string{"user", "group", "role", "priority", "bearer-token", "dry-run", "yes"} {
		if grant.Flags().Lookup(flag) == nil {
			t.Fatalf("auth grant ws flag --%s missing", flag)
		}
	}
	roles, _, _ := authCmd.Find([]string{"roles", "ws"})
	for _, args := range [][]string{nil, {"ws-demo"}} {
		if err := roles.Args(roles, args); err != nil {
			t.Fatalf("auth roles ws rejected %v: %v", args, err)
		}
	}
	command, _, err := newAuthCmd().Find([]string{"login"})
	if err != nil {
		t.Fatalf("find auth login: %v", err)
	}
	if command.Flags().Lookup("environment") != nil {
		t.Fatal("auth login unexpectedly exposes a local environment flag")
	}
	flag := rootCmd.PersistentFlags().Lookup("environment")
	if flag == nil || flag.Shorthand != "e" {
		t.Fatal("global --environment/-e flag missing")
	}
}
