package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegularFileLimitsAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadRegularFile(t.Context(), path, 5)
	if err != nil || string(data) != "hello" {
		t.Fatalf("read = %q, %v", data, err)
	}
	if _, err := ReadRegularFile(t.Context(), path, 4); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("size limit = %v", err)
	}
	if _, err := OpenRegularFile(filepath.Dir(path)); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("directory accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadRegularFile(ctx, path, 5); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v", err)
	}
}

func TestReadRegularFileInRootIsBoundedAndRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadRegularFileInRoot(t.Context(), root, "data", 5)
	if err != nil || string(data) != "hello" {
		t.Fatalf("root read=%q err=%v", data, err)
	}
	if _, err := ReadRegularFileInRoot(t.Context(), root, "data", 4); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("unbounded root read: %v", err)
	}
	if _, err := ReadRegularFileInRoot(t.Context(), root, ".", 5); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("directory allowed: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ReadRegularFileInRoot(t.Context(), root, "escape", 10); err == nil {
		t.Fatal("symlink escaped root")
	}
}
