package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"nekocode/bot"
	"nekocode/tui"
)

const panicLogDir = "/tmp/nekocode"

func main() {
	defer recoverPanic()

	if len(os.Args) > 1 {
		runNonInteractive()
		return
	}

	tui.Run()
}

func runNonInteractive() {
	b := bot.New()
	input := strings.Join(os.Args[1:], " ")

	fmt.Println("> " + input)

	output, err := b.RunAgent(input, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(output)
}

func recoverPanic() {
	if r := recover(); r != nil {
		stack := string(debug.Stack())
		if err := os.MkdirAll(panicLogDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create panic log dir: %v\n\nPANIC: %v\nStack:\n%s\n", err, r, stack)
			return
		}
		logPath := fmt.Sprintf("%s/nekocode-panic-%d.log", panicLogDir, time.Now().Unix())
		if err := os.WriteFile(logPath, fmt.Appendf(nil, "PANIC: %v\n\nStack:\n%s", r, stack), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write panic log: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "\nPANIC: %v\nStack saved to %s\n", r, logPath)
	}
}
