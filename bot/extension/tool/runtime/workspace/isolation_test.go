package workspace

import (
	"path/filepath"
	"testing"
)

func TestRelativePathsStayInsideTheirOwningProject(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	for _, primary := range []string{one, two} {
		m := New(primary, []Root{{Path: "extra", Access: AccessReadOnly}})
		for _, check := range []func(string) (string, Root, bool, error){m.CheckRead, m.CheckReadLexical, m.CheckWrite} {
			path, _, allowed, err := check("file.txt")
			if err != nil || !allowed || path != filepath.Join(primary, "file.txt") {
				t.Fatalf("path=%q allowed=%v err=%v", path, allowed, err)
			}
		}
		path, _, allowed, err := m.CheckRead("extra/file.txt")
		if err != nil || !allowed || path != filepath.Join(primary, "extra/file.txt") {
			t.Fatalf("extra path=%q allowed=%v err=%v", path, allowed, err)
		}
		other := two
		if primary == two {
			other = one
		}
		if _, _, allowed, err := m.CheckWrite(filepath.Join(other, "file.txt")); err != nil || allowed {
			t.Fatalf("cross-project authority allowed=%v err=%v", allowed, err)
		}
	}
}

func TestFallbackPrimaryHonorsWorkspaceEnvironment(t *testing.T) {
	primary := t.TempDir()
	t.Setenv("NEKOCODE_WORKSPACE", primary)
	t.Setenv("NEKOCODE_EXTRA_DIRS", "")
	m := New("", nil)
	t.Setenv("NEKOCODE_WORKSPACE", t.TempDir())
	if m.Primary() != primary {
		t.Fatalf("primary changed: %q", m.Primary())
	}
	path, _, allowed, err := m.CheckRead("file.txt")
	if err != nil || !allowed || path != filepath.Join(primary, "file.txt") {
		t.Fatalf("path=%q allowed=%v err=%v", path, allowed, err)
	}
}

func TestChangingPrimaryDropsSessionScopedRoots(t *testing.T) {
	first, second, grant := t.TempDir(), t.TempDir(), t.TempDir()
	m := New(first, nil)
	if _, err := m.AddSessionRoot(grant, AccessReadWrite); err != nil {
		t.Fatal(err)
	}
	m.Configure(second, nil)
	if _, _, allowed, err := m.CheckWrite(filepath.Join(grant, "file.txt")); err != nil || allowed {
		t.Fatalf("old session grant survived project change: allowed=%v err=%v", allowed, err)
	}
}
