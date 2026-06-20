package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePath(t *testing.T) {
	td := t.TempDir()
	rel := filepath.Join(td, "sub")
	os.MkdirAll(rel, 0755)

	resolved, err := ValidatePath(rel)
	if err != nil {
		t.Fatalf("ValidatePath: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("expected absolute path, got %q", resolved)
	}

	if _, err := ValidatePath(filepath.Join(td, "nonexistent")); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReadNormalizedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("\x1b[31ma\r\nb\x1b[0m"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadNormalizedFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nb" {
		t.Fatalf("ReadNormalizedFile() = %q", got)
	}
}
