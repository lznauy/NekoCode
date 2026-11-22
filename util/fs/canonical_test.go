package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalPathResolvesMissingDescendants(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := CanonicalPath(filepath.Join(link, "missing", "nested", "file"))
	want, resolveErr := filepath.EvalSymlinks(real)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	want = filepath.Join(want, "missing", "nested", "file")
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestCanonicalPathRejectsBrokenAndCyclicLinks(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "broken")
	if err := os.Symlink(filepath.Join(root, "absent"), broken); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	loop := filepath.Join(root, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{broken, filepath.Join(broken, "nested", "file"), loop} {
		if got, err := CanonicalPath(path); err == nil {
			t.Errorf("resolved invalid link %q to %q", path, got)
		}
	}
}

func TestIsTempRootIncludesBothCanonicalDirectories(t *testing.T) {
	system, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Skip("no system /tmp")
	}
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "tmp")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skip(err)
	}
	t.Setenv("TMPDIR", alias)
	process, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !IsTempRoot(system) || !IsTempRoot(process) || IsTempRoot(filepath.Join(process, "nested")) {
		t.Fatalf("incorrect temp root classification for %q and %q", system, process)
	}
}
