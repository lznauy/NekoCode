package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Regular file.
	os.WriteFile(filepath.Join(src, "regular.txt"), []byte("hello"), 0o644)
	// Subdirectory with file.
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "sub", "nested.txt"), []byte("nested"), 0o644)

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	// Verify regular file.
	data, err := os.ReadFile(filepath.Join(dst, "regular.txt"))
	if err != nil || string(data) != "hello" {
		t.Error("regular file not copied correctly")
	}

	// Verify nested file.
	data, err = os.ReadFile(filepath.Join(dst, "sub", "nested.txt"))
	if err != nil || string(data) != "nested" {
		t.Error("nested file not copied correctly")
	}
}

func TestCopyDirWithSymlinks(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create a real file and symlink to it.
	os.WriteFile(filepath.Join(src, "target.txt"), []byte("target content"), 0o644)
	os.Symlink(filepath.Join(src, "target.txt"), filepath.Join(src, "link-to-file"))

	// Symlink to a directory.
	os.MkdirAll(filepath.Join(src, "real-dir"), 0o755)
	os.WriteFile(filepath.Join(src, "real-dir", "inside.txt"), []byte("inside"), 0o644)
	os.Symlink(filepath.Join(src, "real-dir"), filepath.Join(src, "link-to-dir"))

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	// Symlink to file should be preserved as a symlink.
	linkTarget, err := os.Readlink(filepath.Join(dst, "link-to-file"))
	if err != nil {
		t.Fatalf("link-to-file should be a symlink: %v", err)
	}
	if filepath.Base(linkTarget) != "target.txt" {
		t.Errorf("symlink target = %q, should point to target.txt", linkTarget)
	}

	// Symlink to dir should be preserved as a symlink.
	dirLinkTarget, err := os.Readlink(filepath.Join(dst, "link-to-dir"))
	if err != nil {
		t.Fatalf("link-to-dir should be a symlink: %v", err)
	}
	if filepath.Base(dirLinkTarget) != "real-dir" {
		t.Errorf("symlink target = %q, should point to real-dir", dirLinkTarget)
	}

	// Regular files should still be copied normally.
	data, err := os.ReadFile(filepath.Join(dst, "target.txt"))
	if err != nil || string(data) != "target content" {
		t.Error("regular file not copied correctly alongside symlinks")
	}
}
