package toolutil

import (
	"context"
	"os/exec"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
)

// CaptureCommand bounds memory and disk while stdout/stderr are being read.
// Hitting the cap intentionally cancels the producer and returns an explicitly
// incomplete result. Caller cancellation and other command errors remain errors.
func CaptureCommand(ctx context.Context, bin string, args []string, dir string) (core.ToolOutput, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	store := core.NewOutputStore("")
	if active := core.OutputStoreFromContext(ctx); active != nil {
		store = active
	}
	capture := core.NewOutputCapture(store, core.MaxCapturedOutputBytes, cancel)
	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout, cmd.Stderr = capture, capture
	commandErr := cmd.Run()
	output, err := capture.Finish()
	if err != nil {
		return core.ToolOutput{}, err
	}
	if path := output.Artifact.Path; path != "" {
		if manager, ok := workspace.FromContext(ctx); ok {
			if _, err := manager.AddSessionRoot(path, workspace.AccessReadOnly); err != nil {
				return core.ToolOutput{}, err
			}
		}
	}
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	if capture.Limited() {
		return output, nil
	}
	return output, commandErr
}
