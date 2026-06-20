package plugin

import (
	"strings"
	"testing"
)

func TestRunPluginCommandExpandsPluginRoot(t *testing.T) {
	root := t.TempDir()
	output, truncated, err := runPluginCommand(root, hookAction{
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
