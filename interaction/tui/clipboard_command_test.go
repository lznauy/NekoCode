package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClipboardCommandHelper(t *testing.T) {
	mode := os.Getenv("NEKOCODE_TEST_CLIPBOARD_COMMAND")
	if mode == "" {
		return
	}
	switch mode {
	case "image":
		_, _ = os.Stdout.Write([]byte{0x89, 'P', 'N', 'G', 0, 0xff})
	case "text":
		_, _ = os.Stdout.WriteString("截图 🐱\nsecond line")
	case "none":
		os.Exit(clipboardHelperNoDataExit)
	case "empty":
	case "oversized":
		_, _ = os.Stdout.WriteString(strings.Repeat("x", 4096))
	case "error":
		_, _ = os.Stderr.WriteString("clipboard unavailable")
		os.Exit(2)
	case "timeout":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func TestClipboardCommandResults(t *testing.T) {
	for _, mode := range []string{"image", "text", "none", "empty", "oversized", "error", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			duration := 10 * time.Second
			if mode == "timeout" {
				duration = 50 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), duration)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClipboardCommandHelper$")
			cmd.Env = append(os.Environ(), "NEKOCODE_TEST_CLIPBOARD_COMMAND="+mode)
			data, err := readClipboardCommand(ctx, cmd, clipboardHelperImage, 1024)
			switch mode {
			case "image":
				if err != nil || string(data) != string([]byte{0x89, 'P', 'N', 'G', 0, 0xff}) {
					t.Fatalf("binary changed: %x %v", data, err)
				}
			case "text":
				if err != nil || string(data) != "截图 🐱\nsecond line" {
					t.Fatalf("text changed: %q %v", data, err)
				}
			case "none", "empty":
				if !errors.Is(err, errClipboardNoData) {
					t.Fatalf("no-data lost: %v", err)
				}
			case "oversized":
				if err == nil || data != nil || !strings.Contains(err.Error(), "limit") {
					t.Fatalf("oversized accepted: %d %v", len(data), err)
				}
			case "error":
				if err == nil || !strings.Contains(err.Error(), "clipboard unavailable") {
					t.Fatalf("diagnostic lost: %v", err)
				}
			case "timeout":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline lost: %v", err)
				}
			}
		})
	}
}

func TestWindowsClipboardSelection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL is a Linux environment")
	}
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	if !useWindowsClipboard() {
		t.Fatal("WSL used Linux clipboard")
	}
	t.Setenv("SSH_CONNECTION", "remote connection")
	if useWindowsClipboard() {
		t.Fatal("SSH read Windows host clipboard")
	}
}
func TestWindowsClipboardMissingInterop(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := readWindowsClipboard(t.Context(), clipboardHelperImage, 1024)
	if err == nil || !strings.Contains(err.Error(), "powershell.exe") {
		t.Fatalf("missing interoperability not explained: %v", err)
	}
}
