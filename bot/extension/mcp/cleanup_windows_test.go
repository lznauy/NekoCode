//go:build windows

package mcp

import (
	"io"
	"os/exec"
	"testing"
)

func TestMCPAlreadyExitedProcessRetainsFailedTreeCleanup(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "0")
	configureProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &client{cmd: cmd, stdoutPipe: stdout}
	t.Cleanup(func() { _ = cmd.Wait() })
	if _, err = io.ReadAll(stdout); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	if err = c.Close(); err == nil {
		t.Fatal("parent exit incorrectly certified process-tree cleanup")
	}
	if c.cmd != cmd || !c.stopping || c.treeStopped {
		t.Fatal("failed process-tree cleanup released ownership")
	}
}
