package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryPrecedenceAndPinnedReload(t *testing.T) {
	root := t.TempDir()
	projectDir, userDir, pluginDir := filepath.Join(root, "z-project"), filepath.Join(root, "a-user"), filepath.Join(root, "plugin")
	for _, dir := range []string{projectDir, userDir, pluginDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: hunt\ndescription: override\n---\n"+dir), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := NewWithDirs(nil, nil, 32000, []string{projectDir, userDir})
	m.Load([]string{pluginDir})
	assertSource := func(want string) {
		t.Helper()
		sk, ok := m.Get("hunt")
		if !ok || sk.Dir != want {
			t.Fatalf("hunt=%+v, want source %q", sk, want)
		}
	}
	assertSource(projectDir)
	t.Chdir(t.TempDir())
	m.Reload([]string{pluginDir})
	assertSource(projectDir)
	for _, pair := range [][2]string{{projectDir, userDir}, {userDir, pluginDir}, {pluginDir, ""}} {
		if err := os.Remove(filepath.Join(pair[0], "SKILL.md")); err != nil {
			t.Fatal(err)
		}
		m.Reload([]string{pluginDir})
		assertSource(pair[1])
	}
}
