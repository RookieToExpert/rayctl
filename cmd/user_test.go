package cmd

import (
	"strings"
	"testing"
)

func TestUserGetIncludesWorkloadFlags(t *testing.T) {
	cmd := newUserGetCmd()
	for _, flag := range []string{"jobs", "aid", "ait", "ate", "air", "all"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("user get is missing --%s", flag)
		}
	}
	help := strings.Join([]string{cmd.Short, cmd.Long, cmd.Example}, "\n")
	for _, example := range []string{"--aid", "--ait --ate", "--all"} {
		if !strings.Contains(help, example) {
			t.Fatalf("user get help does not contain %q", example)
		}
	}
}
