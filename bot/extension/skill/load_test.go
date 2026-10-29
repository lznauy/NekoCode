package skill

import (
	"fmt"
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
when_to_use: legacy trigger field should be ignored
context: inline
disable-model-invocation: true
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
	if sk.Context != "inline" || !sk.DisableModelInvocation {
		t.Fatal("supported execution metadata not parsed")
	}
	if sk.Content != "# Test Skill\n\nThis is the test skill body." {
		t.Errorf("content = %q", sk.Content)
	}
	if len(sk.Files) != 1 || !strings.HasSuffix(sk.Files[0], "helper.txt") {
		t.Errorf("files = %v", sk.Files)
	}
}

func TestLoadErrors(t *testing.T) {
	_, err := parseSkillContent("no frontmatter")
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
	_, err = parseSkillContent("---\nname: x\n---")
	if err == nil {
		t.Error("expected error for missing description")
	}
	_, err = parseSkillContent("---\nname: x\n---\nbody")
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Error("expected missing description error")
	}
}

func TestSkillNameRejectsUnicodeWhitespace(t *testing.T) {
	for _, name := range []string{"has space", "has\ttab", "has\nnewline", "has\u00a0nbsp", "has\u2003em-space", "has\u3000ideographic-space", " \t\u00a0"} {
		t.Run(name, func(t *testing.T) {
			_, err := parseSkillContent("---\nname: " + fmt.Sprintf("%q", name) + "\ndescription: test\n---\nbody")
			if err == nil || !strings.Contains(err.Error(), "$ command identifier") || !strings.Contains(err.Error(), "whitespace") {
				t.Fatalf("expected command identifier whitespace error for %q, got %v", name, err)
			}
		})
	}
}

func TestSkillNameAllowsNonWhitespaceCharacters(t *testing.T) {
	const name = "deploy.prod+工具"
	sk, err := parseSkillContent("---\nname: " + name + "\ndescription: test\n---\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != name {
		t.Fatalf("name = %q, want %q", sk.Name, name)
	}
}

func TestDefaultDirs(t *testing.T) {
	dirs := defaultDirs()
	if len(dirs) != 2 {
		t.Errorf("expected 2 default dirs, got %d", len(dirs))
	}
}

func TestBundledSkills(t *testing.T) {
	got := bundledSkills()
	want := map[string][]string{
		"skill-creator": {"Create", "skill"},
		"hunt":          {"Diagnose", "root cause", "debug"},
		"think":         {"architecture", "plan", "before coding"},
		"check":         {"Review", "diff", "after implementation"},
	}
	if len(got) != len(want) {
		t.Fatalf("bundled skills = %d, want %d", len(got), len(want))
	}
	for _, sk := range got {
		triggers, ok := want[sk.Name]
		if !ok {
			t.Errorf("unexpected bundled skill %q", sk.Name)
			continue
		}
		if sk.Description == "" || sk.Content == "" {
			t.Errorf("bundled skill %q is incomplete", sk.Name)
		}
		for _, trigger := range triggers {
			if !strings.Contains(sk.Description, trigger) {
				t.Errorf("bundled skill %q description missing trigger %q", sk.Name, trigger)
			}
		}
		delete(want, sk.Name)
	}
	if len(want) != 0 {
		t.Errorf("missing bundled skills: %v", want)
	}
}

func TestUnsupportedExecutionFields(t *testing.T) {
	for _, field := range []string{"context: fork", "context: unknown", "allowed-tools: []", "agent: ''", "max_steps: 0", "context_window: 0", "context: inline\n<<: {agent: coder}"} {
		t.Run(field, func(t *testing.T) {
			_, err := parseSkillContent("---\nname: x\ndescription: test\n" + field + "\n---\nbody")
			if err == nil || !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("expected explicit unsupported error, got %v", err)
			}
		})
	}
}

func TestBundledCreatorDraftIsLoadable(t *testing.T) {
	const fence = "```yaml\n"
	_, draft, ok := strings.Cut(bundledMetaContent, fence)
	if !ok {
		t.Fatal("missing draft template")
	}
	draft, _, ok = strings.Cut(draft, "```")
	if !ok {
		t.Fatal("unterminated draft template")
	}
	sk, err := parseSkillContent(draft)
	if err != nil {
		t.Fatalf("creator draft cannot load: %v", err)
	}
	if !strings.Contains(sk.Content, "## Steps") || !strings.Contains(sk.Content, "## Output") {
		t.Fatalf("missing workflow scaffold: %s", sk.Content)
	}
}
