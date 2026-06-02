package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func setupPluginFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0755)
	os.MkdirAll(filepath.Join(dir, "skills", "test-skill"), 0755)
	os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name": "test-plugin", "version": "0.1.0", "skills": ["./skills/test-skill"]}`), 0644)
	return dir
}

func TestParseManifest(t *testing.T) {
	dir := setupPluginFixture(t)
	m, err := ParseManifest(dir)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Name != "test-plugin" {
		t.Errorf("name = %q, want %q", m.Name, "test-plugin")
	}
	if m.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", m.Version, "0.1.0")
	}
	if len(m.Skills) != 1 || m.Skills[0] != "./skills/test-skill" {
		t.Errorf("skills = %v, want [./skills/test-skill]", m.Skills)
	}
}

func TestHasManifest(t *testing.T) {
	dir := setupPluginFixture(t)
	if !HasManifest(dir) {
		t.Error("HasManifest should return true for plugin dir")
	}
	if HasManifest("/tmp") {
		t.Error("HasManifest should return false for /tmp")
	}
}

func TestPluginSkillDirs(t *testing.T) {
	dir := setupPluginFixture(t)
	m, err := ParseManifest(dir)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	p := &Plugin{Manifest: *m, Dir: dir}
	dirs := p.SkillDirs()
	if len(dirs) != 1 {
		t.Fatalf("SkillDirs len = %d, want 1", len(dirs))
	}
	expected := filepath.Join(dir, "skills", "test-skill")
	if dirs[0] != expected {
		t.Errorf("SkillDirs[0] = %q, want %q", dirs[0], expected)
	}
}

func TestSkillDirsDefault(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755)
	os.MkdirAll(filepath.Join(dir, "skills", "auto-skill"), 0o755)
	os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name": "auto-skill-plugin"}`), 0o644)

	m, err := ParseManifest(dir)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	p := &Plugin{Manifest: *m, Dir: dir}
	dirs := p.SkillDirs()
	if len(dirs) != 1 {
		t.Fatalf("SkillDirs len = %d, want 1 (auto-detected)", len(dirs))
	}
	if dirs[0] != filepath.Join(dir, "skills") {
		t.Errorf("SkillDirs[0] = %q, want skills/", dirs[0])
	}
}
