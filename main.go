package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"rayctl/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if code := cmd.RequestedProcessExitCode(); code != 0 {
		os.Exit(code)
	}
}
