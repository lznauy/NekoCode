package apistate

import (
	"testing"
	"time"

	"nekocode/common"
)

func TestCommandResult(t *testing.T) {
	if got := CommandResult(true, true); got != common.CmdConfirming {
		t.Fatalf("pending confirm wins, got %v", got)
	}
	if got := CommandResult(false, true); got != common.CmdSessionResumed {
		t.Fatalf("session resumed = %v", got)
	}
	if got := CommandResult(false, false); got != common.CmdHandled {
		t.Fatalf("handled = %v", got)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{500 * time.Millisecond, "0s"},
		{1500 * time.Millisecond, "1.5s"},
		{1234 * time.Millisecond, "1.2s"},
	}
	for _, tt := range tests {
		if got := FormatDuration(tt.d); got != tt.want {
			t.Fatalf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
