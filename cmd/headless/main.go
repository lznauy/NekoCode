// Command headless supports one-shot execution and the NDJSON host protocol.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"nekocode/headless"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"--headless"}
	}
	options, selected, err := headless.ParseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !selected {
		fmt.Fprintln(os.Stderr, "expected headless arguments; use exec for one-shot execution or --headless for a persistent connection")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := headless.RunStdio(ctx, options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(headless.ExitCode(err))
	}
}
