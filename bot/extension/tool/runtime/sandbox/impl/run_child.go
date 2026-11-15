//go:build linux

package impl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"nekocode/bot/extension/tool/runtime/toolutil"
)

// runChild spawns a re-exec'd sandbox helper (self) with the given args/env,
// captures stdout+stderr, enforces timeout with process-group kill, and
// classifies the result.
//
// unavailableMatch inspects the captured stderr and the cmd.Run() error and
// reports whether the failure means "the sandbox backend itself is unusable"
// (in which case an UnavailableError is returned so the caller can fall back
// to a weaker backend). Pass nil to never classify as unavailable.
//
// unavailReason is the prefix used in the UnavailableError reason string.
//
// This is the shared core of runNativeBash and runLandlockBash; the two
// backends differ only in argv layout, environment, SysProcAttr, and how
// "sandbox unusable" is detected.
func runChild(
	ctx context.Context,
	self string,
	args []string,
	env []string,
	dir string,
	timeout time.Duration,
	sysproc *syscall.SysProcAttr,
	unavailableMatch func(stderr string, runErr error) bool,
	unavailReason string,
	files ...[]*os.File,
) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, self, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.SysProcAttr = sysproc
	if len(files) > 0 {
		cmd.ExtraFiles = files[0]
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("sandbox execution failed: %v", err)
	}

	// Kill the whole process group on timeout/cancel so nested children
	// (e.g. `bash -c 'sleep 30'`) cannot outlive the deadline. Registered
	// after Start so cmd.Process is stable when the callback reads it.
	stop := context.AfterFunc(cmdCtx, func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	})
	defer stop()

	if err := cmd.Wait(); err != nil {
		outStr := truncateCapturedOutput(toolutil.StripAnsi(stdout.String()))
		errStr := truncateCapturedOutput(toolutil.StripAnsi(stderr.String()))
		if unavailableMatch != nil && unavailableMatch(stderr.String(), err) {
			return "", UnavailableError{Reason: fmt.Sprintf("%s: %v", unavailReason, err)}
		}
		if cmdCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %v: %v%s", timeout, err, formatOutputSections(outStr, errStr))
		}
		return "", fmt.Errorf("sandbox execution failed: %v%s", err, formatOutputSections(outStr, errStr))
	}

	out := toolutil.StripAnsi(stdout.String())
	return truncateCapturedOutput(out), nil
}

// formatOutputSections builds the "stdout: ... stderr: ..." tail for error
// messages, omitting sections whose content is empty so the error stays
// compact and the relevant output is obvious.
func formatOutputSections(stdout, stderr string) string {
	var b strings.Builder
	if stdout != "" {
		fmt.Fprintf(&b, "\nstdout: %s", stdout)
	}
	if stderr != "" {
		fmt.Fprintf(&b, "\nstderr: %s", stderr)
	}
	return b.String()
}
