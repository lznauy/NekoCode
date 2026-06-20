package projectctx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectContextIncludesNestedRules(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "app")
	rules := filepath.Join(root, ".nekocode", "rules")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rules, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "extra.md"), []byte("included guidance"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "NEKOCODE.md"), []byte("root guidance\n@./extra.md"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rules, "01-style.md"), []byte("style guidance"), 0644); err != nil {
		t.Fatal(err)
	}

	out := LoadProjectContext(sub)
	for _, want := range []string{"<project-context>", "root guidance", "included guidance", "style guidance"} {
		if !strings.Contains(out, want) {
			t.Fatalf("context missing %q:\n%s", want, out)
		}
	}
}

func TestLoadProjectContextEmptyWhenNoFiles(t *testing.T) {
	if out := LoadProjectContext(t.TempDir()); out != "" {
		t.Fatalf("expected empty context, got %q", out)
	}
}
