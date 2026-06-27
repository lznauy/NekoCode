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
