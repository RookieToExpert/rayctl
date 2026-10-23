package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestListLongCommandInventory(t *testing.T) {
	want := map[string]bool{
		"afs list": true, "aid list": true, "air job list": true,
		"ecs list": true, "group list": true, "natgw list": true,
		"node list": true, "queue node list": true, "subnet list": true,
		"user list": true, "vc node list": true, "vpc list": true,
	}
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if flag := command.Flags().Lookup("long"); command.Name() == "list" && flag != nil {
			path := strings.TrimPrefix(command.CommandPath(), rootCmd.Name()+" ")
			if !want[path] || flag.Shorthand != "l" {
				t.Fatalf("uncovered list -l command: %s", path)
			}
			if !strings.HasPrefix(flag.Usage, "仅显示") {
				t.Fatalf("%s does not describe incremental columns: %s", path, flag.Usage)
			}
			t.Logf("%s: %s", path, flag.Usage)
			delete(want, path)
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
	if len(want) != 0 {
		t.Fatalf("missing list commands: %v", want)
	}
}
