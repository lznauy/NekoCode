package authority

import (
	"os"
	"path/filepath"
	"testing"
)

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestWritableProtectionIncludesAncestorsAndSymlinks(t *testing.T) {
	home := isolateHome(t)
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, home, filepath.Dir(root), filepath.Join(root, "project", "permissions.json")} {
		if err := CheckWritable(path); err == nil {
			t.Errorf("unprotected authority overlap %s", path)
		}
	}
	alias := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip(err)
	}
	if err := CheckWritable(filepath.Join(alias, "missing", "nested", "file")); err == nil {
		t.Fatal("symlink descendant bypassed protection")
	}
	if err := CheckWritable(filepath.Join(home, "ordinary-project")); err != nil {
		t.Fatal(err)
	}
}
func TestStorePathRejectsReplacedAuthorityDirectory(t *testing.T) {
	home := isolateHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".nekocode"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(home, ".nekocode", "permissions")); err != nil {
		t.Skip(err)
	}
	if _, err := StorePath(t.TempDir()); err == nil {
		t.Fatal("followed authority symlink")
	}
}
func TestStorePathsIsolateProjects(t *testing.T) {
	isolateHome(t)
	a, err := StorePath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := StorePath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("projects share authority path")
	}
}
