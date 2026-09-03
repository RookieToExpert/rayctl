package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestReadOnlyQueryCommandsAcceptMultipleIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		root *cobra.Command
		path []string
	}{
		{name: "afs get", root: newAFSCmd(), path: []string{"get"}},
		{name: "vpc get", root: newVPCCmd(), path: []string{"get"}},
		{name: "subnet get", root: newSubnetCmd(), path: []string{"get"}},
		{name: "natgw get", root: newNATGatewayCmd(), path: []string{"get"}},
		{name: "vc node list", root: newVCCmd(), path: []string{"node", "list"}},
		{name: "user get", root: newUserCmd(), path: []string{"get"}},
		{name: "aid get", root: newAIDCmd(), path: []string{"get"}},
		{name: "ait get", root: newAITCmd(), path: []string{"get"}},
		{name: "job get", root: newJobCmd(), path: []string{"get"}},
		{name: "ecp job get", root: newECPCmd(), path: []string{"job", "get"}},
		{name: "auth check user", root: newAuthCmd(), path: []string{"check", "user"}},
		{name: "auth check groups", root: newAuthCmd(), path: []string{"check", "groups"}},
		{name: "auth check afs", root: newAuthCmd(), path: []string{"check", "afs"}},
		{name: "rbac get", root: newRBACCmd(), path: []string{"get"}},
		{name: "node cordon", root: newNodeCmd(), path: []string{"cordon"}},
		{name: "node uncordon", root: newNodeCmd(), path: []string{"uncordon"}},
		{name: "pv get", root: newPVCmd(), path: []string{"get"}},
		{name: "pvc get", root: newPVCCmd(), path: []string{"get"}},
		{name: "ecs get", root: newECSCmd(), path: []string{"get"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, _, err := tt.root.Find(tt.path)
			if err != nil {
				t.Fatalf("find command: %v", err)
			}
			if err := command.Args(command, []string{"first", "second"}); err != nil {
				t.Fatalf("multiple identifiers rejected: %v", err)
			}
		})
	}
}

func TestResourceGetCommandsRequireIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		root *cobra.Command
		path []string
	}{
		{name: "afs get", root: newAFSCmd(), path: []string{"get"}},
		{name: "vc get", root: newVCCmd(), path: []string{"get"}},
		{name: "cluster get", root: newClusterCmd(), path: []string{"get"}},
		{name: "workspace get", root: newWorkspaceCmd(), path: []string{"get"}},
		{name: "queue get", root: newQueueCmd(), path: []string{"get"}},
		{name: "vpc get", root: newVPCCmd(), path: []string{"get"}},
		{name: "subnet get", root: newSubnetCmd(), path: []string{"get"}},
		{name: "natgw get", root: newNATGatewayCmd(), path: []string{"get"}},
		{name: "policy get", root: newPolicyCmd(), path: []string{"get"}},
		{name: "pv get", root: newPVCmd(), path: []string{"get"}},
		{name: "pvc get", root: newPVCCmd(), path: []string{"get"}},
		{name: "ecs get", root: newECSCmd(), path: []string{"get"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, _, err := tt.root.Find(tt.path)
			if err != nil {
				t.Fatalf("find command: %v", err)
			}
			if err := command.Args(command, nil); err == nil {
				t.Fatal("get command accepted an empty identifier list")
			}
		})
	}
}

func TestResourceListCommandsRejectIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		root *cobra.Command
		path []string
	}{
		{name: "afs list", root: newAFSCmd(), path: []string{"list"}},
		{name: "vc list", root: newVCCmd(), path: []string{"list"}},
		{name: "cluster list", root: newClusterCmd(), path: []string{"list"}},
		{name: "workspace list", root: newWorkspaceCmd(), path: []string{"list"}},
		{name: "queue list", root: newQueueCmd(), path: []string{"list"}},
		{name: "vpc list", root: newVPCCmd(), path: []string{"list"}},
		{name: "subnet list", root: newSubnetCmd(), path: []string{"list"}},
		{name: "natgw list", root: newNATGatewayCmd(), path: []string{"list"}},
		{name: "policy list", root: newPolicyCmd(), path: []string{"list"}},
		{name: "aid list", root: newAIDCmd(), path: []string{"list"}},
		{name: "ait list", root: newAITCmd(), path: []string{"list"}},
		{name: "job list", root: newJobCmd(), path: []string{"list"}},
		{name: "ecp job list", root: newECPCmd(), path: []string{"job", "list"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, _, err := tt.root.Find(tt.path)
			if err != nil {
				t.Fatalf("find command: %v", err)
			}
			if err := command.Args(command, nil); err != nil {
				t.Fatalf("list command rejected an empty argument list: %v", err)
			}
			if err := command.Args(command, []string{"unexpected"}); err == nil {
				t.Fatal("list command accepted a resource identifier")
			}
		})
	}
}

func TestAITAndAIDListExposeCatalogFilters(t *testing.T) {
	for _, tt := range []struct {
		name string
		root *cobra.Command
	}{
		{name: "ait", root: newAITCmd()},
		{name: "aid", root: newAIDCmd()},
		{name: "job", root: newJobCmd()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			command, _, err := tt.root.Find([]string{"list"})
			if err != nil {
				t.Fatalf("find list command: %v", err)
			}
			for _, flag := range []string{"workspace", "queue", "state", "limit", "all"} {
				if command.Flags().Lookup(flag) == nil {
					t.Fatalf("list command is missing --%s", flag)
				}
			}
			for flag, shorthand := range map[string]string{"state": "s", "limit": "n", "all": "A"} {
				if got := command.Flags().Lookup(flag).Shorthand; got != shorthand {
					t.Fatalf("--%s shorthand = %q, want %q", flag, got, shorthand)
				}
			}
			if tt.name == "aid" {
				if flag := command.Flags().Lookup("long"); flag == nil || flag.Shorthand != "l" {
					t.Fatal("aid list is missing --long/-l")
				}
			}
		})
	}
}

func TestSSPPrimaryAndLegacyCommandStructure(t *testing.T) {
	jobGet, _, err := newJobCmd().Find([]string{"get"})
	if err != nil {
		t.Fatalf("find primary job get: %v", err)
	}
	if jobGet.Flags().Lookup("workspace") == nil {
		t.Fatal("primary job get does not expose SSP workspace flag")
	}
	jobList, _, err := newJobCmd().Find([]string{"list"})
	if err != nil {
		t.Fatalf("find primary job list: %v", err)
	}
	for _, flag := range []string{"workspace", "queue", "state", "limit", "all"} {
		if jobList.Flags().Lookup(flag) == nil {
			t.Fatalf("primary job list is missing --%s", flag)
		}
	}

	ecpGet, _, err := newECPCmd().Find([]string{"job", "get"})
	if err != nil {
		t.Fatalf("find ecp job get: %v", err)
	}
	if ecpGet.Flags().Lookup("debug-timing") == nil {
		t.Fatal("ecp job get does not expose ECP timing flag")
	}
	ecpList, _, err := newECPCmd().Find([]string{"job", "list"})
	if err != nil {
		t.Fatalf("find ecp job list: %v", err)
	}
	for flag, shorthand := range map[string]string{"state": "s", "limit": "n", "all": "A"} {
		value := ecpList.Flags().Lookup(flag)
		if value == nil || value.Shorthand != shorthand {
			t.Fatalf("ecp job list --%s shorthand = %v, want %q", flag, value, shorthand)
		}
	}

	airGateway, _, err := newAIRCmd().Find([]string{"gw"})
	if err != nil {
		t.Fatalf("find air gateway alias: %v", err)
	}
	if airGateway.Name() != "gateway" {
		t.Fatalf("air gw resolved to %q, want gateway", airGateway.Name())
	}
}

func TestRBACGetUsesGlobalEnvironmentSelection(t *testing.T) {
	command, _, err := newRBACCmd().Find([]string{"get"})
	if err != nil {
		t.Fatalf("find rbac get: %v", err)
	}
	if command.Flags().Lookup("environment") != nil {
		t.Fatal("rbac get unexpectedly exposes a local environment flag")
	}
	flag := rootCmd.PersistentFlags().Lookup("environment")
	if flag == nil || flag.Shorthand != "e" {
		t.Fatal("global --environment/-e flag missing")
	}
}
