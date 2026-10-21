package search

import (
	"context"
	"nekocode/bot/extension/tool/runtime/core"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
)

func TestGrepTool(t *testing.T) {
	td := testutil.SetupTemp(t)
	g := &GrepTool{}

	out, err := g.Execute(testutil.Context(td), map[string]any{"pattern": "func", "path": td})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if out.Render() == "" {
		t.Error("expected matches for 'func'")
	}

	_, err = g.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing pattern")
	}
}

func TestGrepCapsOutputWhileSearching(t *testing.T) {
	td := t.TempDir()
	source := filepath.Join(td, "huge.txt")
	if err := os.WriteFile(source, []byte(strings.Repeat("needle ", 2*core.MaxCapturedOutputBytes/7)), 0600); err != nil {
		t.Fatal(err)
	}
	store := core.NewOutputStore(filepath.Join(td, "results"))
	ctx := core.WithOutputStore(testutil.Context(td), store)
	output, err := (&GrepTool{}).Execute(ctx, map[string]any{"pattern": "needle", "path": source})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Render()) > core.MaxOutputBytes || !strings.Contains(output.Render(), "incomplete") {
		t.Fatalf("unbounded or unlabeled output: %d bytes", len(output.Render()))
	}
	path := output.Artifact.Path
	info, err := os.Stat(path)
	if err != nil || info.Size() > core.MaxCapturedOutputBytes {
		t.Fatalf("unbounded artifact: %v", err)
	}
}

func TestGrepPreservesNoMatchAndCancellation(t *testing.T) {
	td := testutil.SetupTemp(t)
	output, err := (&GrepTool{}).Execute(testutil.Context(td), map[string]any{"pattern": "not-present-at-all", "path": td})
	if err != nil || output.Render() != "No matches found" {
		t.Fatalf("no match: %q %v", output.Render(), err)
	}
	ctx, cancel := context.WithCancel(testutil.Context(td))
	cancel()
	if _, err := (&GrepTool{}).Execute(ctx, map[string]any{"pattern": "func", "path": td}); err == nil {
		t.Fatal("cancellation was swallowed")
	}
}
