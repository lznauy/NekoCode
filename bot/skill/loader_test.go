package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverAndLoad(t *testing.T) {
	td := t.TempDir()
	sd := filepath.Join(td, "test-skill")
	os.MkdirAll(sd, 0755)
	os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(`---
name: test-skill
description: A test skill
when_to_use: test trigger
allowed-tools:
  - bash
  - read
context: fork
agent: executor
max_steps: 4
token_budget: 8000
---

# Test Skill

This is the test skill body.
`), 0644)
	os.WriteFile(filepath.Join(sd, "helper.txt"), []byte("helper"), 0644)

	paths := discoverSkills([]string{td})
	if len(paths) != 1 {
		t.Fatalf("expected 1 discovered skill, got %d", len(paths))
	}

	sk, err := loadSkill(paths[0])
	if err != nil {
		t.Fatalf("loadSkill: %v", err)
	}

	if sk.Name != "test-skill" {
		t.Errorf("name = %q", sk.Name)
	}
	if sk.Description != "A test skill" {
		t.Errorf("description = %q", sk.Description)
	}
	if sk.WhenToUse != "test trigger" {
		t.Errorf("when_to_use = %q", sk.WhenToUse)
	}
	if sk.Context != "fork" || sk.AgentType != "executor" {
		t.Errorf("context/agent mismatch")
	}
	if len(sk.AllowedTools) != 2 || sk.MaxSteps != 4 || sk.TokenBudget != 8000 {
		t.Errorf("execution fields wrong")
	}
	if sk.Content != "# Test Skill\n\nThis is the test skill body." {
		t.Errorf("content = %q", sk.Content)
	}
	if len(sk.Files) != 1 || !strings.HasSuffix(sk.Files[0], "helper.txt") {
		t.Errorf("files = %v", sk.Files)
	}
}

func TestLoadErrors(t *testing.T) {
	_, err := LoadFromContent("no frontmatter")
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
	_, err = LoadFromContent("---\nname: x\n---")
	if err == nil {
		t.Error("expected error for missing description")
	}
	_, err = LoadFromContent("---\nname: x\n---\nbody")
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Error("expected missing description error")
	}
}

func TestDefaultDirs(t *testing.T) {
	dirs := DefaultDirs()
	if len(dirs) != 2 {
		t.Errorf("expected 2 default dirs, got %d", len(dirs))
	}
}
