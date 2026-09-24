package mcp

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// The opener is bounded and never writes subprocess output into ACP/TUI stdio.
func openAuthorizationBrowser(ctx context.Context, raw string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{raw}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", raw}
	default:
		for _, candidate := range []string{"xdg-open", "gio"} {
			if path, err := exec.LookPath(candidate); err == nil {
				command = path
				if candidate == "gio" {
					args = []string{"open", raw}
				} else {
					args = []string{raw}
				}
				break
			}
		}
	}
	if command == "" {
		return fmt.Errorf("no browser opener available")
	}
	return exec.CommandContext(ctx, command, args...).Run()
}
