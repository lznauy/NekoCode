package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"nekocode/bot/tools"
)

func runCommand(ctx context.Context, cmdStr string, timeout time.Duration) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if dir, err := os.Getwd(); err == nil {
		cmd.Dir = dir
	}

	stop := context.AfterFunc(cmdCtx, func() {
		if cmd.Process != nil {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	defer stop()

	output, err := cmd.CombinedOutput()
	cleaned := tools.StripAnsi(string(output))
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %v: %v\nOutput: %s", timeout, err, cleaned)
		}
		return "", fmt.Errorf("command failed: %v\nOutput: %s", err, cleaned)
	}
	return cleaned, nil
}
