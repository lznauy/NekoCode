package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func SetupTemp(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "a.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(d, "b.go"), []byte("package main\n\nfunc helper() {}\n"), 0644)
	os.MkdirAll(filepath.Join(d, "sub"), 0755)
	os.WriteFile(filepath.Join(d, "sub", "c.txt"), []byte("hello\nworld\n"), 0644)
	return d
}
