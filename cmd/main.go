package main

import (
	"fmt"
	"os"
	"strings"

	"nekocode/bot"
	"nekocode/common"
	"nekocode/tui"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			common.WritePanicLog(r)
		}
	}()

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

