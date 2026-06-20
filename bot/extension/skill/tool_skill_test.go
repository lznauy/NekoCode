package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillToolExecute(t *testing.T) {
	td := t.TempDir()
	sd := filepath.Join(td, "test-skill")
	os.MkdirAll(sd, 0755)
	os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(`---
name: test-skill
description: A test skill
---

# Test Body
`), 0644)

	reg := NewRegistry()
	reg.Load([]string{td})
	tool := NewSkillTool(reg)

	if tool.Name() != "skill" {
		t.Errorf("Name() = %q", tool.Name())
	}

	out, err := tool.Execute(context.Background(), map[string]any{"name": "test-skill"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "<skill_content") || !strings.Contains(out, "# Test Body") {
		t.Errorf("output missing content: %s", out)
	}

	// Missing name.
	_, err = tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing name")
	}

	// Not found.
	_, err = tool.Execute(context.Background(), map[string]any{"name": "nope"})
	if err == nil {
		t.Error("expected error for nonexistent skill")
	}

	// Already loaded.
	reg.MarkLoaded("test-skill")
	out, err = tool.Execute(context.Background(), map[string]any{"name": "test-skill"})
	if err != nil {
		t.Fatalf("already loaded should not error: %v", err)
	}
	if !strings.Contains(out, "already loaded") {
		t.Errorf("expected 'already loaded' message: %s", out)
	}
}
