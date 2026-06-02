package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAndAdd(t *testing.T) {
	dir := t.TempDir()
	hooksPath := filepath.Join(dir, "hooks.json")
	content := `{
  "PostToolUse": [
    {
      "matcher": "Write|Edit",
      "hooks": [
        {
          "type": "command",
          "command": "echo formatted"
        }
      ]
    }
  ],
  "PreToolUse": [
    {
      "matcher": "Bash",
      "hooks": [
        {
          "type": "command",
          "command": "echo validating"
        }
      ]
    }
  ]
}`
	os.WriteFile(hooksPath, []byte(content), 0o644)

	r := NewDeclarativeRegistry()
	if err := r.ParseAndAdd(dir, "hooks.json"); err != nil {
		t.Fatalf("ParseAndAdd: %v", err)
	}

	// Test PostToolUse with matching tool.
	hints := r.PostToolUse("Write", true)
	if len(hints) != 1 {
		t.Fatalf("PostToolUse hints = %d, want 1", len(hints))
	}
	if hints[0].Type != "hook_output" {
		t.Errorf("hint type = %q, want hook_output", hints[0].Type)
	}

	// Test PostToolUse with non-matching tool.
	hints = r.PostToolUse("Read", true)
	if len(hints) != 0 {
		t.Errorf("PostToolUse hints for Read = %d, want 0", len(hints))
	}

	// Test PreToolUse with Bash.
	hints = r.PreToolUse("Bash")
	if len(hints) != 1 {
		t.Fatalf("PreToolUse hints = %d, want 1", len(hints))
	}
}

func TestMatchTool(t *testing.T) {
	tests := []struct {
		matcher  string
		toolName string
		want     bool
	}{
		{"", "anything", true},
		{".*", "anything", true},
		{"Write|Edit", "Write", true},
		{"Write|Edit", "Edit", true},
		{"Write|Edit", "Read", false},
		{"Bash", "Bash", true},
		{"Bash", "bash", false},
	}

	for _, tt := range tests {
		got := matchTool(tt.matcher, tt.toolName)
		if got != tt.want {
			t.Errorf("matchTool(%q, %q) = %v, want %v", tt.matcher, tt.toolName, got, tt.want)
		}
	}
}

func TestPostToolUseFailure(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(`{
  "PostToolUseFailure": [
    {
      "matcher": ".*",
      "hooks": [
        { "type": "command", "command": "echo failed" }
      ]
    }
  ]
}`), 0o644)

	r := NewDeclarativeRegistry()
	r.ParseAndAdd(dir, "hooks.json")

	// PostToolUse(true) should NOT match PostToolUseFailure.
	hints := r.PostToolUse("Write", true)
	if len(hints) != 0 {
		t.Errorf("successful tool should not trigger PostToolUseFailure, got %d hints", len(hints))
	}

	// PostToolUse(false) should match PostToolUseFailure.
	hints = r.PostToolUse("Write", false)
	if len(hints) != 1 {
		t.Errorf("failed tool should trigger PostToolUseFailure, got %d hints", len(hints))
	}
}

func TestHookDefaultTimeout(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(`{
  "PostToolUse": [
    {
      "matcher": ".*",
      "hooks": [
        { "type": "command", "command": "echo no_timeout_specified" }
      ]
    }
  ]
}`), 0o644)

	r := NewDeclarativeRegistry()
	r.ParseAndAdd(dir, "hooks.json")

	// Should still work with default timeout.
	hints := r.PostToolUse("Write", true)
	if len(hints) != 1 {
		t.Errorf("hooks = %d, want 1 (should work with default timeout)", len(hints))
	}
}

func TestHookPipeMatcher(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(`{
  "PostToolUse": [
    {
      "matcher": "Write|Edit|Bash",
      "hooks": [
        { "type": "command", "command": "echo matched" }
      ]
    }
  ]
}`), 0o644)

	r := NewDeclarativeRegistry()
	r.ParseAndAdd(dir, "hooks.json")

	for _, tool := range []string{"Write", "Edit", "Bash"} {
		hints := r.PostToolUse(tool, true)
		if len(hints) != 1 {
			t.Errorf("PostToolUse(%q) = %d hints, want 1", tool, len(hints))
		}
	}
	// Read should not match.
	hints := r.PostToolUse("Read", true)
	if len(hints) != 0 {
		t.Errorf("PostToolUse(Read) = %d hints, want 0", len(hints))
	}
}

func TestHookMultiplePlugins(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	os.WriteFile(filepath.Join(dir1, "hooks.json"), []byte(`{
  "PostToolUse": [
    {
      "matcher": "Write",
      "hooks": [
        { "type": "command", "command": "echo plugin1" }
      ]
    }
  ]
}`), 0o644)

	os.WriteFile(filepath.Join(dir2, "hooks.json"), []byte(`{
  "PostToolUse": [
    {
      "matcher": "Write",
      "hooks": [
        { "type": "command", "command": "echo plugin2" }
      ]
    }
  ]
}`), 0o644)

	r := NewDeclarativeRegistry()
	r.ParseAndAdd(dir1, "hooks.json")
	r.ParseAndAdd(dir2, "hooks.json")

	hints := r.PostToolUse("Write", true)
	if len(hints) != 2 {
		t.Errorf("PostToolUse hints = %d, want 2 (both plugins)", len(hints))
	}
}

func TestExpandVars(t *testing.T) {
	result := expandVars("cd ${CLAUDE_PLUGIN_ROOT} && make", "/home/user/plugin")
	if result != "cd /home/user/plugin && make" {
		t.Errorf("expandVars = %q", result)
	}

	result = expandVars("ls ${PLUGIN_ROOT}/scripts", "/tmp/foo")
	if result != "ls /tmp/foo/scripts" {
		t.Errorf("expandVars = %q", result)
	}
}

func TestRemoveConfigForPlugin(t *testing.T) {
	r := NewDeclarativeRegistry()

	dir1 := t.TempDir()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir1, "hooks.json"), []byte(`{
  "PostToolUse": [{"matcher": "Write", "hooks": [{"type": "command", "command": "echo p1"}]}]
}`), 0o644)
	os.WriteFile(filepath.Join(dir2, "hooks.json"), []byte(`{
  "PostToolUse": [{"matcher": "Edit", "hooks": [{"type": "command", "command": "echo p2"}]}]
}`), 0o644)

	r.ParseAndAdd(dir1, "hooks.json")
	r.ParseAndAdd(dir2, "hooks.json")

	// Both plugins contribute.
	hints := r.PostToolUse("Write", true)
	if len(hints) != 1 {
		t.Errorf("before remove: Write hints = %d, want 1", len(hints))
	}
	hints = r.PostToolUse("Edit", true)
	if len(hints) != 1 {
		t.Errorf("before remove: Edit hints = %d, want 1", len(hints))
	}

	// Remove plugin 1.
	r.RemoveConfigForPlugin(dir1)

	hints = r.PostToolUse("Write", true)
	if len(hints) != 0 {
		t.Errorf("after remove: Write hints = %d, want 0", len(hints))
	}
	hints = r.PostToolUse("Edit", true)
	if len(hints) != 1 {
		t.Errorf("after remove: Edit hints = %d, want 1 (still active)", len(hints))
	}

	// Remove non-existent — no-op.
	r.RemoveConfigForPlugin("/nonexistent")
}
