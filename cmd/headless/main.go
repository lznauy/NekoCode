// Command headless serves the NDJSON protocol without loading TUI dependencies.
package main

import (
	"context"
	"log"
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
		log.Fatal(err)
	}
	if !selected {
		log.Fatal("expected headless arguments; use --headless for a persistent connection")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := headless.RunStdio(ctx, options); err != nil {
		log.Fatal(err)
	}
}
