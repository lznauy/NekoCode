package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistry(t *testing.T) {
	td := t.TempDir()
	sd := filepath.Join(td, "s1")
	os.MkdirAll(sd, 0755)
	os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(`---
name: s1
description: first skill
---

# Body
`), 0644)

	reg := NewRegistry()
	reg.Load([]string{td})

	// Get.
	sk, ok := reg.Get("s1")
	if !ok || sk.Name != "s1" {
		t.Fatal("Get failed")
	}
	if _, ok := reg.Get("nope"); ok {
		t.Error("expected false for missing skill")
	}

	// List.
	if len(reg.List()) != 1 {
		t.Errorf("List = %d", len(reg.List()))
	}

	// MarkLoaded / IsLoaded / ClearLoaded / LoadedSet.
	reg.MarkLoaded("s1")
	if !reg.IsLoaded("s1") {
		t.Error("expected loaded")
	}
	if len(reg.LoadedSet()) != 1 {
		t.Error("LoadedSet wrong")
	}
	reg.ClearLoaded()
	if reg.IsLoaded("s1") {
		t.Error("expected not loaded after clear")
	}

	// RegisterBundled.
	bundled := []*Skill{{Name: "b", Description: "bundled"}}
	reg.RegisterBundled(bundled)
	if sk, _ := reg.Get("b"); sk == nil {
		t.Error("bundled skill not registered")
	}
}

func TestBuildSkillListText(t *testing.T) {
	skills := []*Skill{
		{Name: "deploy", Description: "deploy app", WhenToUse: "when deploying"},
		{Name: "review", Description: "review code"},
	}

	text := BuildSkillListText(skills, nil, 64000)
	if text == "" || !strings.Contains(text, "deploy") || !strings.Contains(text, "review") {
		t.Error("missing skill names")
	}
	if !strings.Contains(text, "when deploying") {
		t.Error("missing when_to_use")
	}

	// Loaded filtering.
	text = BuildSkillListText(skills, map[string]bool{"deploy": true}, 64000)
	if strings.Contains(text, "deploy") {
		t.Error("loaded skill should be excluded")
	}

	// All loaded.
	if BuildSkillListText(skills, map[string]bool{"deploy": true, "review": true}, 64000) != "" {
		t.Error("expected empty when all loaded")
	}

	// Edge cases.
	if BuildSkillListText(nil, nil, 0) != "" {
		t.Error("nil skills should return empty")
	}
	if BuildSkillListText([]*Skill{}, nil, 0) != "" {
		t.Error("empty skills should return empty")
	}
}

func TestFormatForContext(t *testing.T) {
	sk := &Skill{
		Name: "deploy", Content: "# Deploy\n\nbuild",
		Dir: "/tmp/skills/deploy", Files: []string{"script.sh"},
	}
	text := FormatForContext(sk)
	if !strings.Contains(text, `<skill_content name="deploy">`) {
		t.Error("missing tag")
	}
	if !strings.Contains(text, "# Deploy") {
		t.Error("missing body")
	}
	if !strings.Contains(text, "script.sh") {
		t.Error("missing file")
	}
}
