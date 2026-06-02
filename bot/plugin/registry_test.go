package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestPlugin creates a temporary plugin directory with a valid plugin.json manifest.
func newTestPlugin(t *testing.T, name, version string) string {
	t.Helper()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, name)
	metaDir := filepath.Join(pluginDir, ".claude-plugin")
	os.MkdirAll(metaDir, 0o755)
	os.WriteFile(filepath.Join(metaDir, "plugin.json"),
		[]byte(`{"name":"`+name+`","version":"`+version+`","description":"test plugin","skills":["skills"]}`),
		0o644)
	// Ensure skills dir exists so SkillDirs() returns something.
	os.MkdirAll(filepath.Join(pluginDir, "skills"), 0o755)
	return pluginDir
}

func TestInstallLocal(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	src := newTestPlugin(t, "test-plugin", "1.0.0")

	r := NewRegistry(DefaultDirs())
	p, err := r.Install(src)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if p.Name != "test-plugin" {
		t.Errorf("name = %q, want test-plugin", p.Name)
	}
	if !p.Enabled {
		t.Error("should be enabled after install")
	}

	if !HasManifest(p.Dir) {
		t.Error("installed plugin should have manifest")
	}

	r2 := NewRegistry(DefaultDirs())
	r2.LoadAll()
	if p2, ok := r2.Get("test-plugin"); !ok {
		t.Error("plugin should persist after reload")
	} else if !p2.Enabled {
		t.Error("plugin should be enabled")
	}

	dirs := r.SkillDirs()
	if len(dirs) != 1 {
		t.Errorf("SkillDirs len = %d, want 1", len(dirs))
	}

	list := r.List()
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	if err := r.Uninstall("test-plugin"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, ok := r.Get("test-plugin"); ok {
		t.Error("plugin should be gone after uninstall")
	}
}

func TestEnableDisable(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	src := newTestPlugin(t, "test-plugin", "1.0.0")
	r := NewRegistry(DefaultDirs())
	_, err := r.Install(src)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := r.Disable("test-plugin"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	p, _ := r.Get("test-plugin")
	if p.Enabled {
		t.Error("should be disabled")
	}
	if len(r.SkillDirs()) != 0 {
		t.Error("disabled plugin should not contribute skill dirs")
	}

	if err := r.Enable("test-plugin"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	p, _ = r.Get("test-plugin")
	if !p.Enabled {
		t.Error("should be enabled")
	}
	if len(r.SkillDirs()) != 1 {
		t.Error("enabled plugin should contribute skill dirs")
	}
}

func TestRegistryPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	src := newTestPlugin(t, "test-plugin", "1.0.0")
	r := NewRegistry(DefaultDirs())
	_, err := r.Install(src)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	r2 := NewRegistry(DefaultDirs())
	r2.LoadAll()
	p, ok := r2.Get("test-plugin")
	if !ok {
		t.Fatal("plugin should persist via registry.json")
	}
	if p.Name != "test-plugin" || !p.Enabled {
		t.Errorf("persisted state: name=%s enabled=%v", p.Name, p.Enabled)
	}

	r.Uninstall("test-plugin")
}

func TestRegistryOverrideProjectLevel(t *testing.T) {
	tmpHome := t.TempDir()
	tmpProject := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", oldHome)

	userPluginDir := filepath.Join(tmpHome, ".nekocode", "plugins", "my-same-plugin")
	os.MkdirAll(filepath.Join(userPluginDir, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(userPluginDir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"same-plugin","version":"1.0.0","description":"user version"}`), 0o644)

	projPluginDir := filepath.Join(tmpProject, ".nekocode", "plugins", "my-project-plugin")
	os.MkdirAll(filepath.Join(projPluginDir, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(projPluginDir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"same-plugin","version":"2.0.0","description":"project version"}`), 0o644)

	r := NewRegistry([]string{
		filepath.Join(tmpProject, ".nekocode", "plugins"),
		filepath.Join(tmpHome, ".nekocode", "plugins"),
	})
	r.LoadAll()

	p, ok := r.Get("same-plugin")
	if !ok {
		t.Fatal("plugin not found")
	}
	if p.Version != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0 (project-level should override)", p.Version)
	}
}

func TestLooksLikeGitRepo(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"user/repo", true},
		{"org-name/project-name", true},
		{"github.com/user/repo", false},
		{"/local/path", false},
		{"./relative/path", false},
		{"user", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := looksLikeGitRepo(tt.input); got != tt.want {
			t.Errorf("looksLikeGitRepo(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestRepoName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/user/repo", "user-repo"},
		{"https://github.com/user/repo.git", "user-repo"},
		{"https://gitlab.com/org/project/", "org-project"},
	}
	for _, tt := range tests {
		if got := repoName(tt.url); got != tt.want {
			t.Errorf("repoName(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestPluginInfo_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	src := newTestPlugin(t, "test-plugin", "1.0.0")
	r := NewRegistry(DefaultDirs())
	_, err := r.Install(src)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	r.Disable("test-plugin")

	p, _ := r.Get("test-plugin")
	if p.Enabled {
		t.Error("should be disabled")
	}
	if len(r.SkillDirs()) != 0 {
		t.Error("disabled plugin should not contribute skill dirs")
	}

	r.Uninstall("test-plugin")
}

func TestInstallInvalidSource(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	r := NewRegistry(DefaultDirs())
	_, err := r.Install("/nonexistent/path/to/plugin")
	if err == nil {
		t.Error("should fail for nonexistent path")
	}
}

func TestUninstallNonexistent(t *testing.T) {
	r := NewRegistry(DefaultDirs())
	err := r.Uninstall("nonexistent-plugin")
	if err == nil {
		t.Error("should fail for nonexistent plugin")
	}
}

func TestEmptyRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	r := NewRegistry(DefaultDirs())
	r.LoadAll()
	if len(r.List()) != 0 {
		t.Error("empty registry should have no plugins")
	}
	if len(r.SkillDirs()) != 0 {
		t.Error("empty registry should have no skill dirs")
	}
}

func TestPreviewFromPath(t *testing.T) {
	src := newTestPlugin(t, "preview-plugin", "2.0.0")
	r := NewRegistry(DefaultDirs())
	p, err := r.PreviewFromPath(src)
	if err != nil {
		t.Fatalf("PreviewFromPath: %v", err)
	}
	if p.Name != "preview-plugin" {
		t.Errorf("name = %q", p.Name)
	}
	if p.Version != "2.0.0" {
		t.Errorf("version = %q", p.Version)
	}
	if p.Dir != src {
		t.Errorf("dir = %q, want %q", p.Dir, src)
	}
}

func TestPreviewFromPath_NoManifest(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(DefaultDirs())
	_, err := r.PreviewFromPath(dir)
	if err == nil {
		t.Error("should fail for directory without manifest")
	}
}
