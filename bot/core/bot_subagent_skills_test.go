package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDelegatedSkillsRejectManualOnly(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	dir := filepath.Join(root, ".nekocode", "skills", "manual")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: manual\ndescription: demo\ndisable-model-invocation: true\n---\nmanual workflow"), 0600); err != nil {
		t.Fatal(err)
	}
	b := newPersistTestBot(t)
	if _, ok := b.ext.Skill("manual"); !ok {
		t.Fatal("manual skill missing")
	}
	// Profile defaults and task.skills share this resolver before child execution.
	for _, names := range [][]string{{"manual"}, {"check", "manual"}} {
		if _, _, err := b.delegatedSkills(names); err == nil || !strings.Contains(err.Error(), "manual-only") {
			t.Fatalf("delegated load: %v", err)
		}
	}
	if _, _, err := b.delegatedSkills([]string{"check", "check"}); err != nil {
		t.Fatal(err)
	}
}
