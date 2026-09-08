package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func TestVCRenewFailureDoesNotPrintUsageOrDuplicateError(t *testing.T) {
	root := &cobra.Command{Use: "rayctl"}
	vc := &cobra.Command{Use: "vc"}
	renew := newVCRenewCmd()
	want := errors.New("credential unavailable")
	renew.RunE = func(*cobra.Command, []string) error { return want }
	vc.AddCommand(renew)
	root.AddCommand(vc)
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetArgs([]string{"vc", "renew"})
	if err := root.Execute(); !errors.Is(err, want) {
		t.Fatalf("error must reach main for nonzero exit: %v", err)
	}
	if out.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("unexpected Cobra output: %s %s", out.String(), stderr.String())
	}
}
