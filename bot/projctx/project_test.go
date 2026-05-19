package projctx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildContext(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "test.md")
	os.WriteFile(p, []byte("# Title\n\nContent here."), 0644)

	result := buildContext([]string{p})
	if !strings.Contains(result, "<project-context>") {
		t.Error("missing wrapper")
	}
	if !strings.Contains(result, "Content here") {
		t.Error("missing content")
	}
	if !strings.Contains(result, "test.md") {
		t.Error("missing file tag")
	}
}

func TestBuildContextEmpty(t *testing.T) {
	if buildContext(nil) != "" {
		t.Error("expected empty for nil")
	}
	if buildContext([]string{}) != "" {
		t.Error("expected empty")
	}
}

func TestBuildContextWithInclude(t *testing.T) {
	td := t.TempDir()
	main := filepath.Join(td, "main.md")
	sub := filepath.Join(td, "sub.md")
	os.WriteFile(main, []byte("# Main\n\n@./sub.md"), 0644)
	os.WriteFile(sub, []byte("included content"), 0644)

	result := buildContext([]string{main})
	if !strings.Contains(result, "included content") {
		t.Errorf("include not resolved: %s", result)
	}
}

func TestIsTextFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"Dockerfile", true},
		{".gitignore", true},
		{"image.png", false},
		{"binary", false},
	}
	for _, tt := range tests {
		if got := isTextFile(tt.path); got != tt.want {
			t.Errorf("isTextFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestLooksLikePath(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"./foo", true},
		{"main.go", true},
		{"param", false},
		{"@param", false},
	}
	for _, tt := range tests {
		if got := looksLikePath(tt.ref); got != tt.want {
			t.Errorf("looksLikePath(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}
