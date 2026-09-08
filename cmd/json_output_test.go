package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func TestAllCommandsMergeGlobalFlags(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		t.Run(command.CommandPath(), func(t *testing.T) {
			command.InheritedFlags()
			if err := command.ParseFlags(nil); err != nil {
				t.Fatal(err)
			}
		})
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
}

func TestAuditGlobalOutputFlags(t *testing.T) {
	for _, help := range []bool{false, true} {
		root := &cobra.Command{Use: "rayctl"}
		root.PersistentFlags().StringP("output", "o", "table", "output format")
		logs := newLogsCmd()
		root.AddCommand(logs)
		audit, _, err := root.Find([]string{"logs", "audit"})
		if err != nil {
			t.Fatal(err)
		}
		called := false
		audit.RunE = func(cmd *cobra.Command, _ []string) error {
			called = true
			operation, _ := cmd.Flags().GetString("operation-type")
			format, _ := cmd.Flags().GetString("output")
			if operation != "deleteVCJobs" || format != "json" {
				t.Fatalf("operation=%q output=%q", operation, format)
			}
			return nil
		}
		args := []string{"logs", "audit", "--operation-type", "deleteVCJobs", "-o", "json"}
		if help {
			args = append(args, "--help")
		}
		var buffer bytes.Buffer
		root.SetOut(&buffer)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if called == help {
			t.Fatal("unexpected audit handler execution")
		}
		if help && (!strings.Contains(buffer.String(), "--operation-type") || !strings.Contains(buffer.String(), "--output")) {
			t.Fatal(buffer.String())
		}
	}
}

func TestJSONCommandSuppressesHeadingsAndKeepsResults(t *testing.T) {
	for _, args := range [][]string{{"-o", "json", "get", "a", "b"}, {"get", "a", "b", "-o", "json"}} {
		root := &cobra.Command{Use: "test"}
		root.PersistentFlags().StringP("output", "o", "table", "")
		root.AddCommand(&cobra.Command{Use: "get", RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "heading must not leak")
			for _, name := range args {
				output.PrintSSPAIDDetail(&service.SSPAIDGetResult{Name: name}, false, false)
			}
			return nil
		}})
		installJSONCommands(root)
		var buffer bytes.Buffer
		root.SetOut(&buffer)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		var payload struct{ Items []struct{ Name string } }
		if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
			t.Fatal(err, buffer.String())
		}
		if len(payload.Items) != 2 || payload.Items[1].Name != "b" {
			t.Fatal(buffer.String())
		}
	}
}

func TestJSONWriteCommandDoesNotExecute(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().StringP("output", "o", "table", "")
	grant := &cobra.Command{Use: "grant"}
	called := false
	grant.AddCommand(&cobra.Command{Use: "user", RunE: func(*cobra.Command, []string) error { called = true; return nil }})
	root.AddCommand(grant)
	installJSONCommands(root)
	root.SetArgs([]string{"grant", "user", "-o", "json"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err == nil || called {
		t.Fatal("write command executed under unsupported output mode")
	}
}

func TestJSONPolicyListRunHandler(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().StringP("output", "o", "table", "")
	root.AddCommand(newPolicyListCmd())
	installJSONCommands(root)
	var buffer bytes.Buffer
	root.SetOut(&buffer)
	root.SetArgs([]string{"list", "-o", "json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct{ Items []any }
	if err := json.Unmarshal(buffer.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) == 0 {
		t.Fatal("missing policies")
	}
}

func TestJSONCommandFailureDoesNotAppendUsage(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().StringP("output", "o", "table", "")
	root.AddCommand(&cobra.Command{Use: "get", RunE: func(cmd *cobra.Command, args []string) error {
		output.PrintSSPAIDDetail(&service.SSPAIDGetResult{Name: "success"}, false, false)
		return errors.New("second target failed")
	}})
	installJSONCommands(root)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"get", "a", "b", "-o", "json"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected partial failure")
	}
	if !json.Valid(out.Bytes()) {
		t.Fatal("invalid stdout", out.String())
	}
}
