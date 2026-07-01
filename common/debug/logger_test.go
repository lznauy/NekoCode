package debug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoggerWritesDebugLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	logger := NewLogger(path)
	logger.now = func() time.Time {
		return time.Date(2026, 6, 19, 1, 2, 3, 4_000_000, time.UTC)
	}

	logger.Log(1, "[DBG]", "", "hello %s", "world")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "01:02:03.004 [DBG]") || !strings.Contains(got, "hello world") {
		t.Fatalf("unexpected log line: %q", got)
	}
}

func TestRotateIfNeeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	if err := os.WriteFile(path, []byte("abcdef"), 0644); err != nil {
		t.Fatal(err)
	}
	rotateIfNeeded(path, 3)
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
}
