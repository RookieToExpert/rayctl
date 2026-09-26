package cmd

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestLoginUsernamePromptStopsOnCanceledContext(t *testing.T) {
	t.Setenv("PJLAB_USERNAME", "")
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	cmd := &cobra.Command{}
	cmd.SetIn(input)
	cmd.SetOut(io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		_, err := loginForCommand(ctx, cmd, nil, "", "", false, false)
		completed <- err
	}()
	cancel()
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("login error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("username prompt did not stop after cancellation")
	}
}

func TestReadLineWithContext(t *testing.T) {
	line, err := readLineWithContext(context.Background(), strings.NewReader("yes\n"))
	if err != nil || line != "yes\n" {
		t.Fatalf("read line = %q, err = %v", line, err)
	}
}
