package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"rayctl/pkg/output"
)

func installListSummaries(command *cobra.Command) {
	for _, child := range command.Commands() {
		installListSummaries(child)
	}
	if command.RunE == nil || !isListCommand(command) {
		return
	}
	run := command.RunE
	command.RunE = func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("output")
		if format != "" && format != "table" {
			return run(cmd, args)
		}
		reported := false
		err := output.WithListSummary(cmd.OutOrStdout(), func() error {
			err := run(cmd, args)
			if err != nil {
				// Keep the actual error and nonzero exit, but print it before
				// the final returned-row count rather than after that footer.
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				reported = true
			}
			return err
		})
		if reported {
			return &reportedListError{err: err}
		}
		return err
	}
}

func isListCommand(command *cobra.Command) bool {
	for parent := command; parent != nil; parent = parent.Parent() {
		if parent.Name() == "list" {
			return true
		}
	}
	return false
}

type reportedListError struct{ err error }

func (e *reportedListError) Error() string { return e.err.Error() }
func (e *reportedListError) Unwrap() error { return e.err }

// ErrorAlreadyReported prevents main from printing a list error a second time,
// after the footer. Other commands retain their existing error reporting.
func ErrorAlreadyReported(err error) bool {
	var reported *reportedListError
	return errors.As(err, &reported)
}
