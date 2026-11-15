//go:build windows

package processtree

import (
	"os/exec"
	"testing"
)

func TestAlreadyExitedProcessDoesNotHideTreeKillFailure(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Force tree termination to fail independently of taskkill's exit timing.
	t.Setenv("PATH", t.TempDir())
	if err := Kill(cmd.Process); err == nil {
		t.Fatal("parent exit incorrectly certified process-tree cleanup")
	}
}
