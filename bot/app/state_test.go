package app

import (
	"testing"

	"nekocode/common"
)

func TestCommandResult(t *testing.T) {
	if got := commandResult(true, true); got != common.CmdConfirming {
		t.Fatalf("pending confirm wins, got %v", got)
	}
	if got := commandResult(false, true); got != common.CmdSessionResumed {
		t.Fatalf("session resumed = %v", got)
	}
	if got := commandResult(false, false); got != common.CmdHandled {
		t.Fatalf("handled = %v", got)
	}
}
