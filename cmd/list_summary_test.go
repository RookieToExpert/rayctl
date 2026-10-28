package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func captureListCommand(t *testing.T, root *cobra.Command, args []string) (string, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "list-output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = file, file
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	root.SetOut(file)
	root.SetErr(file)
	root.SetArgs(args)
	err = root.Execute()
	if err != nil && !ErrorAlreadyReported(err) {
		fmt.Fprintln(file, err) // Same final error handling as main.
	}
	if _, seekErr := file.Seek(0, 0); seekErr != nil {
		t.Fatal(seekErr)
	}
	text, readErr := io.ReadAll(file)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(text), err
}

func TestEveryRegisteredListGetsOneSummaryAfterWarnings(t *testing.T) {
	// Keep the real command names, aliases and runnable parents, while replacing
	// network queries with deterministic printers. Exercise the entire tree.
	var clone func(*cobra.Command) *cobra.Command
	clone = func(original *cobra.Command) *cobra.Command {
		command := &cobra.Command{Use: original.Use, Aliases: original.Aliases, SilenceErrors: true, SilenceUsage: true}
		if original.RunE != nil || original.Run != nil {
			command.RunE = func(cmd *cobra.Command, _ []string) error {
				output.PrintSSPQueueList(&service.SSPQueueListResult{Items: make([]service.SSPQueueItem, 2)})
				output.PrintSSPQueueList(&service.SSPQueueListResult{})
				output.PrintSSPQueueList(&service.SSPQueueListResult{Items: make([]service.SSPQueueItem, 1)})
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: after all tables")
				return nil
			}
		}
		for _, child := range original.Commands() {
			command.AddCommand(clone(child))
		}
		return command
	}
	root := clone(rootCmd)
	root.PersistentFlags().StringP("output", "o", "table", "")
	installJSONCommands(root)
	installListSummaries(root)
	lists := 0
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command.RunE != nil {
			args := strings.Fields(strings.TrimPrefix(command.CommandPath(), root.Name()+" "))
			text, err := captureListCommand(t, root, args)
			if err != nil {
				t.Fatalf("%s: %v", command.CommandPath(), err)
			}
			// The expected set is independent of the wrapper's ancestor walk.
			list := false
			for _, arg := range args {
				list = list || arg == "list"
			}
			if list {
				lists++
				if !strings.HasSuffix(strings.TrimSpace(text), "本次输出 3 条。") || strings.Count(text, "本次输出 ") != 1 {
					t.Fatalf("%s:\n%s", command.CommandPath(), text)
				}
			} else if strings.Contains(text, "本次输出 ") {
				t.Fatalf("non-list command changed: %s:\n%s", command.CommandPath(), text)
			}
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
	if lists < 24 {
		t.Fatalf("missing registered list paths: tested %d", lists)
	}
	for _, args := range [][]string{{"air", "gw", "list"}, {"queue", "wl", "list", "a", "b"}, {"ws", "wl", "list", "a"}, {"vc", "wl", "list", "a"}} {
		text, err := captureListCommand(t, root, args)
		if err != nil || !strings.HasSuffix(strings.TrimSpace(text), "本次输出 3 条。") {
			t.Fatalf("alias %v: %v\n%s", args, err, text)
		}
	}
}

func TestListSummaryErrorsPrecedeFooterAndPreserveExitError(t *testing.T) {
	for _, cause := range []error{errors.New("workspace lookup denied"), context.Canceled} {
		for _, rendered := range []bool{false, true} {
			root := &cobra.Command{Use: "test", SilenceErrors: true, SilenceUsage: true}
			root.AddCommand(&cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error {
				if rendered {
					output.PrintSSPQueueList(&service.SSPQueueListResult{Items: make([]service.SSPQueueItem, 2)})
				}
				return cause
			}})
			installListSummaries(root)
			text, err := captureListCommand(t, root, []string{"list"})
			if !errors.Is(err, cause) || !ErrorAlreadyReported(err) || strings.Count(text, cause.Error()) != 1 {
				t.Fatalf("error=%v output=%s", err, text)
			}
			if rendered {
				if !strings.HasSuffix(strings.TrimSpace(text), "本次输出 2 条（查询未完整完成）。") {
					t.Fatal(text)
				}
			} else if strings.Contains(text, "本次输出") {
				t.Fatalf("failed lookup reported an empty list: %s", text)
			}
		}
	}
}

func TestListSummaryPreservesJSONAndDoesNotReportItsErrors(t *testing.T) {
	for _, queryErr := range []error{nil, errors.New("partial query")} {
		encode := func(summary bool) (string, error) {
			root := &cobra.Command{Use: "test", SilenceErrors: true, SilenceUsage: true}
			root.PersistentFlags().StringP("output", "o", "table", "")
			root.AddCommand(&cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error {
				output.PrintSSPQueueList(&service.SSPQueueListResult{Items: make([]service.SSPQueueItem, 2)})
				return queryErr
			}})
			installJSONCommands(root)
			if summary {
				installListSummaries(root)
			}
			var out, stderr bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&stderr)
			root.SetArgs([]string{"list", "-o", "json"})
			err := root.Execute()
			if ErrorAlreadyReported(err) || stderr.Len() != 0 {
				t.Fatal(err, stderr.String())
			}
			return out.String(), err
		}
		before, beforeErr := encode(false)
		after, afterErr := encode(true)
		if before != after || !errors.Is(beforeErr, queryErr) || !errors.Is(afterErr, queryErr) || strings.Contains(after, "本次输出") {
			t.Fatal(before, after, afterErr)
		}
		var payload struct {
			Items   []any
			Summary struct{ Returned int }
		}
		if err := json.Unmarshal([]byte(after), &payload); err != nil || len(payload.Items) != 2 || payload.Summary.Returned != 2 {
			t.Fatal(after, err)
		}
	}
}
