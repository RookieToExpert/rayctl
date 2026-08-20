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
		{name: "job get", root: newJobCmd(), path: []string{"get"}},
		{name: "ecp job get", root: newECPCmd(), path: []string{"job", "get"}},
		{name: "ssp aid get", root: newSSPCmd(), path: []string{"aid", "get"}},
		{name: "ssp job get", root: newSSPCmd(), path: []string{"job", "get"}},
		{name: "auth check user", root: newAuthCmd(), path: []string{"check", "user"}},
		{name: "auth check groups", root: newAuthCmd(), path: []string{"check", "groups"}},
		{name: "auth check afs", root: newAuthCmd(), path: []string{"check", "afs"}},
		{name: "rbac get", root: newRBACCmd(), path: []string{"get"}},
		{name: "node cordon", root: newNodeCmd(), path: []string{"cordon"}},
		{name: "node uncordon", root: newNodeCmd(), path: []string{"uncordon"}},
		{name: "pv check", root: newPVCmd(), path: []string{"check"}},
		{name: "pvc check", root: newPVCCmd(), path: []string{"check"}},
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

func TestSSPPrimaryAndLegacyCommandStructure(t *testing.T) {
	jobGet, _, err := newJobCmd().Find([]string{"get"})
	if err != nil {
		t.Fatalf("find primary job get: %v", err)
	}
	if jobGet.Flags().Lookup("workspace") == nil {
		t.Fatal("primary job get does not expose SSP workspace flag")
	}

	ecpGet, _, err := newECPCmd().Find([]string{"job", "get"})
	if err != nil {
		t.Fatalf("find ecp job get: %v", err)
	}
	if ecpGet.Flags().Lookup("debug-timing") == nil {
		t.Fatal("ecp job get does not expose ECP timing flag")
	}

	airGateway, _, err := newAIRCmd().Find([]string{"gw"})
	if err != nil {
		t.Fatalf("find air gateway alias: %v", err)
	}
	if airGateway.Name() != "gateway" {
		t.Fatalf("air gw resolved to %q, want gateway", airGateway.Name())
	}
}

func TestRBACGetSupportsEnvironmentSelection(t *testing.T) {
	command, _, err := newRBACCmd().Find([]string{"get"})
	if err != nil {
		t.Fatalf("find rbac get: %v", err)
	}
	flag := command.Flags().Lookup("environment")
	if flag == nil {
		t.Fatal("rbac get --environment/-v flag missing")
	}
	if flag.Shorthand != "v" {
		t.Fatalf("rbac get environment shorthand = %q, want v", flag.Shorthand)
	}
}
