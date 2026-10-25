package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPluginCommandExpandsPluginRoot(t *testing.T) {
	root := t.TempDir()
	output, truncated, err := runPluginCommandEvent(root, Event{}, hookAction{
		Type:    "command",
		Command: "printf %s ${PLUGIN_ROOT}",
		Timeout: 1000,
	})
	if err != nil {
		t.Fatalf("runPluginCommand error: %v", err)
	}
	if truncated {
		t.Fatal("short command output should not be truncated")
	}
	if strings.TrimSpace(output) != root {
		t.Fatalf("output = %q, want plugin root %q", output, root)
	}
}

func TestCommandOutputIsTruncated(t *testing.T) {
	output, truncated, err := runPluginCommandEvent(t.TempDir(), Event{}, hookAction{Command: "awk 'BEGIN { for (i=0; i<20000; i++) printf \"x\" }'", Timeout: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || !strings.HasPrefix(output, strings.Repeat("x", maxPluginOutputBytes)) || len(output) > maxPluginOutputBytes+100 {
		t.Fatalf("output length=%d truncated=%v", len(output), truncated)
	}
}

func TestPluginRootIsShellData(t *testing.T) {
	root := filepath.Join(t.TempDir(), "$(printf injected)")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	out, _, err := runPluginCommandEvent(root, Event{}, hookAction{Command: `printf '%s' "${PLUGIN_ROOT}"`})
	if err != nil || out != root {
		t.Fatalf("plugin root was interpreted as shell code: %q %v", out, err)
	}
}
