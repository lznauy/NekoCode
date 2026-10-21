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

	reg := newRegistry()
	reg.Load([]string{td})
	tool := newSkillTool(reg)

	if tool.Name() != "skill" {
		t.Errorf("Name() = %q", tool.Name())
	}
	params := tool.Parameters()
	if len(params) != 1 || len(params[0].Enum) != 1 || params[0].Enum[0] != "test-skill" {
		t.Fatalf("skill schema does not expose available names: %+v", params)
	}

	out, err := tool.Execute(context.Background(), map[string]any{"name": "test-skill"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.Render(), "<skill_content") || !strings.Contains(out.Render(), "# Test Body") {
		t.Errorf("output missing content: %s", out.Render())
	}
	if reg.IsLoaded("test-skill") {
		t.Fatal("tool return must not mark delivery before context accepts it")
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
	if !strings.Contains(out.Render(), "<skill_content") || !strings.Contains(out.Render(), "# Test Body") {
		t.Errorf("loaded skill should be recoverable after compaction: %s", out.Render())
	}
}

func TestOversizedSkillIsNotLoaded(t *testing.T) {
	reg := newRegistry()
	reg.Register(&Skill{Name: "large", Content: strings.Repeat("instruction\n", 4000)})
	output, err := newSkillTool(reg).Execute(context.Background(), map[string]any{"name": "large"})
	if err == nil || output.Render() != "" || reg.IsLoaded("large") {
		t.Fatalf("oversized skill must be rejected intact, got %d bytes, err=%v", len(output.Render()), err)
	}
}
