package plugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

	if err := copyDir(context.Background(), src, dst); err != nil {
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

func TestCopyDirPreservesExecutableMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable permission bits")
	}
	src, dst := t.TempDir(), t.TempDir()
	for _, mode := range []os.FileMode{0700, 0755, 0600} {
		path := filepath.Join(src, "server")
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho ready\n"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := copyDir(context.Background(), src, dst); err != nil {
			t.Fatal(err)
		}
		installed := filepath.Join(dst, "server")
		info, err := os.Stat(installed)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("copied mode = %v, want %o (stat: %v)", info, mode, err)
		}
		if mode&0100 != 0 {
			if out, err := exec.Command(installed).CombinedOutput(); err != nil || string(out) != "ready\n" {
				t.Fatalf("installed executable failed: %v %s", err, out)
			}
		}
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

	if err := copyDir(context.Background(), src, dst); err != nil {
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

func TestCopyDirHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := copyDir(ctx, t.TempDir(), t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copyDir error = %v, want context.Canceled", err)
	}
}
