package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripAnsi(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hello", "hello"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"no ansi here", "no ansi here"},
	}
	for _, tt := range tests {
		if got := StripAnsi(tt.in); got != tt.want {
			t.Errorf("StripAnsi(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidatePath(t *testing.T) {
	td := t.TempDir()
	rel := filepath.Join(td, "sub")
	os.MkdirAll(rel, 0755)

	resolved, err := ValidatePath(rel)
	if err != nil {
		t.Fatalf("ValidatePath: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("expected absolute path, got %q", resolved)
	}

	// Non-existent path should still resolve (no error for non-existence).
	_, err = ValidatePath(filepath.Join(td, "nonexistent"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewToolHTTPClient(t *testing.T) {
	c := NewToolHTTPClient(0)
	if c == nil {
		t.Error("nil client")
	}
	if c.Timeout != 0 {
		t.Error("expected zero timeout")
	}
}
