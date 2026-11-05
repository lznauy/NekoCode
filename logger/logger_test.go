package logger

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

	logger.Log(2, "DBG", "", "hello %s", "world")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "01:02:03.004 DBG") ||
		!strings.Contains(got, "logger_test.go:") ||
		!strings.Contains(got, " | hello world") ||
		strings.Contains(got, "logger.go:") {
		t.Fatalf("unexpected log line: %q", got)
	}
}

func TestPackageLogUsesCallerLocation(t *testing.T) {
	old := defaultLogger
	defer func() { defaultLogger = old }()

	path := filepath.Join(t.TempDir(), "debug.log")
	defaultLogger = NewLogger(path)
	defaultLogger.now = func() time.Time {
		return time.Date(2026, 6, 19, 1, 2, 3, 4_000_000, time.UTC)
	}

	Log("public %s", "entry")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "logger_test.go:") ||
		!strings.Contains(got, " | public entry") ||
		strings.Contains(got, "logger.go:") {
		t.Fatalf("unexpected package log line: %q", got)
	}
}

func TestRotateIfNeeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	if err := os.WriteFile(path, []byte("abcdef"), 0644); err != nil {
		t.Fatal(err)
	}
	rotateIfNeeded(path, 3)
	info, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("rotated file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestLoggerRotatesWhileOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	logger := NewLogger(path)
	t.Cleanup(func() {
		if logger.file != nil {
			_ = logger.file.Close()
		}
	})
	logger.Log(2, "DBG", "", "first")
	if err := logger.file.Truncate(maxSize + 1); err != nil {
		t.Fatal(err)
	}
	logger.Log(2, "DBG", "", "after rotation")
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("active log was not rotated: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "after rotation") || strings.Contains(string(data), "first") {
		t.Fatalf("unexpected current log: %q", data)
	}
}
