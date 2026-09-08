package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"rayctl/pkg/output"
)

var globalOutput string

func installJSONCommands(command *cobra.Command) {
	for _, child := range command.Commands() {
		installJSONCommands(child)
	}
	if command.RunE == nil && command.Run != nil {
		run := command.Run
		command.RunE = func(cmd *cobra.Command, args []string) error { run(cmd, args); return nil }
		command.Run = nil
	}
	if command.RunE == nil {
		return
	}
	// Metrics and health retain their published schemas and metrics CSV support.
	if strings.Contains(command.CommandPath(), " metrics ") || command.Name() == "health" {
		return
	}
	run := command.RunE
	command.RunE = func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("output")
		if format == "" || format == "table" {
			return run(cmd, args)
		}
		if format != "json" {
			return fmt.Errorf("unsupported --output %q; expected table or json", format)
		}
		// Cobra otherwise appends Usage to stdout after a query error.
		cmd.SilenceUsage = true
		allowed := false
		for parent := cmd; parent != nil; parent = parent.Parent() {
			if parent.Name() == "grant" || parent.Name() == "remove" || parent.Name() == "create" {
				return fmt.Errorf("%s does not support JSON output", cmd.CommandPath())
			}
			if parent.Name() == "check" || parent.Name() == "roles" || parent.Name() == "get" || parent.Name() == "list" {
				allowed = true
			}
		}
		switch cmd.Name() {
		case "get", "list", "describe", "usage", "check", "roles", "user", "groups", "audit", "workload":
		default:
			if !allowed {
				return fmt.Errorf("%s does not support JSON output", cmd.CommandPath())
			}
		}
		writer := cmd.OutOrStdout()
		cmd.SetOut(io.Discard)
		defer cmd.SetOut(writer)
		session := output.BeginJSON()
		defer output.EndJSON()
		err := run(cmd, args)
		isList := cmd.Name() == "list" || cmd.Name() == "usage" || len(args) > 1
		return errors.Join(err, session.Write(writer, isList, len(args), err))
	}
}
